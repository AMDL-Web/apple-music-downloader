// Command amdl-desktop is the AMDL desktop app: a native MyGo window over an
// amdl backend, either a remote one or one it runs in-process.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.SetName("AMDL")
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "AMDL",
			Width:   1280,
			Height:  800,
			Content: ui.View(func(c *ui.Context) { ui.Text(c, "AMDL") }),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
