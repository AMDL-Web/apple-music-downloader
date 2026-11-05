package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecRefusesBadArguments(t *testing.T) {
	movie := filepath.Join(t.TempDir(), "a.mov")
	writeFile(t, movie, "not a movie")
	for _, args := range [][]string{
		{"rec", movie},
		{"rec", movie, "260x110"},
		{"rec", movie, "0x110+1+1"},
		{"rec", "-threshold", "300", movie, "10x10+0+0"},
		{"rec", "-frob", movie, "10x10+0+0"},
		{"rec", filepath.Join(t.TempDir(), "none.mov"), "10x10+0+0"},
	} {
		var out, errw strings.Builder
		if code := run(args, &out, &errw); code != 2 {
			t.Errorf("framecheck %q: exit %d, want 2 (%s)", args, code, errw.String())
		}
	}
}

// A dark bar that comes to rest, holds still for a third of a second and
// then drops 10 pixels in one frame is a snap at the frame it drops.
func TestRecFindsASettleThenSnap(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	movie := filepath.Join(dir, "snap.mov")
	// At 60 fps: the bar eases from y 40 to 10 over frames 0-9, rests at
	// 10 to frame 29, then sits at 20.
	y := "if(lt(n,10),40-3*n,if(lt(n,30),10,20))"
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=white:s=96x80:r=60:d=1",
		"-f", "lavfi", "-i", "color=c=black:s=50x12:r=60:d=1",
		"-filter_complex", "[0][1]overlay=x=20:y='"+y+"':eval=frame,format=yuv420p", "-c:v", "libx264", "-qp", "0", movie)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make the film: %v %s", err, out)
	}
	strips := filepath.Join(dir, "strips")
	var out, errw strings.Builder
	if code := run([]string{"rec", "-out", strips, movie, "96x80+0+0"}, &out, &errw); code != 1 {
		t.Fatalf("exit %d, want 1 for a step: %s%s", code, out.String(), errw.String())
	}
	// The overlay counts its frames from one, so the drop shows in frame 29.
	if !strings.Contains(out.String(), "1 steps\nsnap at frame 29: moved (+0, +10) px") {
		t.Errorf("output %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(strips, "step-029.png")); err != nil {
		t.Errorf("no strip: %v", err)
	}
}
