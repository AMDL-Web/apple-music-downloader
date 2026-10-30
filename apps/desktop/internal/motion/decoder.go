package motion

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// frameReader reads raw RGBA frames of one size from r, each dur long. It
// implements ui.VideoDecoder without Rewind.
type frameReader struct {
	r   io.Reader
	img *image.RGBA
	dur time.Duration
}

func newFrameReader(r io.Reader, w, h int, dur time.Duration) *frameReader {
	return &frameReader{r: r, img: image.NewRGBA(image.Rect(0, 0, w, h)), dur: dur}
}

// Next reads the next frame into the one image it reuses: the player
// copies each frame.
func (f *frameReader) Next() (ui.VideoFrame, error) {
	if _, err := io.ReadFull(f.r, f.img.Pix); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			err = io.EOF
		}
		return ui.VideoFrame{}, err
	}
	return ui.VideoFrame{Image: f.img, Duration: f.dur}, nil
}

// decoder plays a local video file through ffmpeg, scaled to w×h. ffmpeg
// loops the file itself, so the frames never end and Rewind is seldom
// needed; it blocks on the pipe while the player is not asking for
// frames, which keeps a paused or hidden video idle.
type decoder struct {
	ffmpeg, file string
	w, h         int
	dur          time.Duration

	mu     sync.Mutex
	cmd    *exec.Cmd
	frames *frameReader
}

func newDecoder(ffmpeg, file string, w, h int, fps float64) (*decoder, error) {
	if fps <= 0 {
		fps = 30
	}
	d := &decoder{ffmpeg: ffmpeg, file: file, w: w, h: h, dur: time.Duration(float64(time.Second) / fps)}
	if err := d.start(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *decoder) start() error {
	cmd := exec.Command(d.ffmpeg, "-nostdin", "-v", "error", "-stream_loop", "-1", "-i", d.file,
		"-an", "-vf", "scale="+strconv.Itoa(d.w)+":"+strconv.Itoa(d.h), "-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1")
	hideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("motion: ffmpeg: %w", err)
	}
	d.mu.Lock()
	d.cmd, d.frames = cmd, newFrameReader(out, d.w, d.h, d.dur)
	d.mu.Unlock()
	return nil
}

// Next decodes the next frame.
func (d *decoder) Next() (ui.VideoFrame, error) {
	d.mu.Lock()
	f := d.frames
	d.mu.Unlock()
	if f == nil {
		return ui.VideoFrame{}, io.EOF
	}
	return f.Next()
}

// Rewind starts ffmpeg again from the file's start.
func (d *decoder) Rewind() error {
	d.stop()
	return d.start()
}

// Close stops ffmpeg.
func (d *decoder) Close() error {
	d.stop()
	return nil
}

func (d *decoder) stop() {
	d.mu.Lock()
	cmd := d.cmd
	d.cmd, d.frames = nil, nil
	d.mu.Unlock()
	if cmd == nil {
		return
	}
	// Killed on purpose: the error Wait reports then says only that.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
