package app

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// scrollbarsAlways reports whether System Settings shows scroll bars
// always. Automatic, the default, leaves it to the pointing device; it is
// read as the trackpad's overlay scrollers, as is a failed read.
func scrollbarsAlways() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "defaults", "read", "-g", "AppleShowScrollBars").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Always"
}
