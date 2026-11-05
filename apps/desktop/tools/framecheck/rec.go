package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"amdl/apps/desktop/internal/filmcheck"
)

const recUsage = `usage: framecheck rec [-threshold 60] [-light] [-least 2] [-out <dir>] <movie> <W>x<H>+<X>+<Y>`

// rec measures one element in a screen recording, at the recording's own
// frames (a 120 Hz Retina recording keeps every frame that changed):
// where the ink of the region W×H at X,Y lies in each frame, and the snaps
// and jumps among those places (filmcheck.Steps), with a strip of the
// frames around each written to dir. Ink is pixels darker than threshold,
// or with light lighter than 255 less it. It needs ffmpeg and ffprobe.
func rec(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("rec", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	threshold := fs.Int("threshold", 60, "")
	light := fs.Bool("light", false, "")
	least := fs.Float64("least", 2, "")
	dir := fs.String("out", ".", "")
	bad := func(why string) int {
		if why != "" {
			fmt.Fprintln(errw, "framecheck rec:", why)
		}
		fmt.Fprintln(errw, recUsage)
		return 2
	}
	if err := fs.Parse(args); err != nil {
		return bad(err.Error())
	}
	if fs.NArg() != 2 {
		return bad("")
	}
	movie := fs.Arg(0)
	var w, h, x, y int
	if n, err := fmt.Sscanf(fs.Arg(1), "%dx%d+%d+%d", &w, &h, &x, &y); n != 4 || err != nil || w <= 0 || h <= 0 || x < 0 || y < 0 {
		return bad("the region is <W>x<H>+<X>+<Y>, as 260x110+1060+440")
	}
	if *threshold <= 0 || *threshold >= 255 || *least <= 0 {
		return bad("-threshold is 1 to 254 and -least above 0")
	}
	if st, err := os.Stat(movie); err != nil || st.IsDir() {
		return bad("no recording at " + movie)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(errw, "framecheck rec:", err)
		return 1
	}
	fps, frames, times, err := readMovie(movie, w, h, x, y)
	if err != nil {
		fmt.Fprintln(errw, "framecheck rec:", err)
		return 1
	}
	isInk := func(v uint8) bool { return int(v) < *threshold }
	if *light {
		isInk = func(v uint8) bool { return int(v) > 255-*threshold }
	}
	inks := make([]filmcheck.Ink, len(frames))
	fmt.Fprintf(out, "%d frames at %.0f fps nominal, region %dx%d+%d+%d\n", len(frames), fps, w, h, x, y)
	fmt.Fprintln(out, "frame\tt (s)\tink\ttop\tbottom\tleft\tright")
	for i, f := range frames {
		inks[i] = filmcheck.InkOf(f, w, w, h, isInk)
		k := inks[i]
		if k.N == 0 {
			fmt.Fprintf(out, "%d\t%.3f\t0\t-\t-\t-\t-\n", i, times[i])
			continue
		}
		fmt.Fprintf(out, "%d\t%.3f\t%d\t%d\t%d\t%d\t%d\n", i, times[i], k.N, k.Top, k.Bottom, k.Left, k.Right)
	}
	steps := filmcheck.Steps(inks, times, 2/fps, *least)
	fmt.Fprintf(out, "%d steps\n", len(steps))
	for _, s := range steps {
		strip := filepath.Join(*dir, fmt.Sprintf("step-%03d.png", s.Frame))
		if err := writeStrip(strip, frames, w, h, s.Frame-3, s.Frame+2); err != nil {
			fmt.Fprintln(errw, "framecheck rec:", err)
			return 1
		}
		fmt.Fprintf(out, "%s at t=%.3f s; frames %d to %d in %s\n", s, times[s.Frame], max(s.Frame-3, 0), min(s.Frame+2, len(frames)-1), strip)
	}
	if len(steps) > 0 {
		return 1
	}
	return 0
}

var ptsTime = regexp.MustCompile(`pts_time:\s*([0-9.]+)`)

// readMovie decodes the region of every frame the recording keeps, as
// grey, with its time, and the stream's nominal frame rate.
func readMovie(path string, w, h, x, y int) (fps float64, frames [][]uint8, times []float64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	rate, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ffprobe: %w", err)
	}
	num, den, ok := strings.Cut(strings.TrimSpace(string(rate)), "/")
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if !ok || err1 != nil || err2 != nil || n <= 0 || d <= 0 {
		return 0, nil, nil, fmt.Errorf("ffprobe gave the frame rate %q", rate)
	}
	fps = n / d
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "info", "-nostdin", "-i", path, "-map", "0:v:0", "-fps_mode", "passthrough",
		"-vf", fmt.Sprintf("crop=%d:%d:%d:%d,format=gray,showinfo", w, h, x, y), "-f", "rawvideo", "-pix_fmt", "gray", "pipe:1")
	var raw, log bytes.Buffer
	cmd.Stdout, cmd.Stderr = &raw, &log
	if err := cmd.Run(); err != nil {
		return 0, nil, nil, fmt.Errorf("ffmpeg: %w: %s", err, lastLines(log.String(), 4))
	}
	sc := bufio.NewScanner(&log)
	for sc.Scan() {
		if line := sc.Text(); strings.Contains(line, "Parsed_showinfo") {
			if m := ptsTime.FindStringSubmatch(line); m != nil {
				t, _ := strconv.ParseFloat(m[1], 64) // the pattern admits only a number
				times = append(times, t)
			}
		}
	}
	size := w * h
	if raw.Len()%size != 0 || raw.Len()/size != len(times) || len(times) < 3 {
		return 0, nil, nil, errors.New("ffmpeg's frames and their times do not match; is the region inside the recording?")
	}
	for i := range len(times) {
		frames = append(frames, raw.Bytes()[i*size:(i+1)*size])
	}
	return fps, frames, times, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[max(len(lines)-n, 0):], " | ")
}

// writeStrip lays frames from to to of the region side by side at their
// own size, 6 pixels apart.
func writeStrip(path string, frames [][]uint8, w, h, from, to int) error {
	from, to = max(from, 0), min(to, len(frames)-1)
	const gap = 6
	n := to - from + 1
	img := image.NewGray(image.Rect(0, 0, n*w+(n-1)*gap, h))
	for i := range img.Pix {
		img.Pix[i] = 128
	}
	for k := range n {
		f := frames[from+k]
		for y := range h {
			copy(img.Pix[y*img.Stride+k*(w+gap):], f[y*w:(y+1)*w])
		}
	}
	fh, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(fh, img); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}
