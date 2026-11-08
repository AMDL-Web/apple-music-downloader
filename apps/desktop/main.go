// Command amdl-desktop is the AMDL desktop app: a native window over an amdl
// backend, either a remote one or one it runs in-process.
package main

import (
	"flag"
	"log"
	"runtime"

	"amdl/apps/desktop/internal/app"
)

func main() {
	var opts app.Options
	flag.StringVar(&opts.DataDir, "data", "", "keep settings and the embedded backend's files in this directory")
	flag.StringVar(&opts.Remote, "remote", "", "use the amdl at this address, without saving it")
	flag.StringVar(&opts.Wrapper, "wrapper", "", "run the backend here with this wrapper, without saving it")
	flag.StringVar(&opts.Theme, "theme", "", "force light or dark")
	flag.StringVar(&opts.Shots, "shots", "", "capture every screen into this directory, then quit")
	flag.StringVar(&opts.Submit, "submit", "", "with -shots, download this link first")
	flag.BoolVar(&opts.Demo, "demo", false, "run on a made-up backend in memory, as the browser build does")
	flag.Parse()
	// A browser has nothing else to run on.
	opts.Demo = opts.Demo || runtime.GOOS == "js"
	if err := app.Run(opts); err != nil {
		log.Fatal(err)
	}
}
