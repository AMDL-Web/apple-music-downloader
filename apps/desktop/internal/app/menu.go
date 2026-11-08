package app

import (
	"runtime"

	"github.com/egoist/mygo"
)

// syncMenu installs the macOS menu bar when the sidebar's item would read
// otherwise: the standard App, Edit and Window menus, and View with Hide
// Sidebar or Show Sidebar under Control-Command-S above Enter Full Screen,
// as the system apps have it. Elsewhere the window draws no menu bar and
// the view's own shortcut toggles the sidebar.
func (a *App) syncMenu() {
	if runtime.GOOS != "darwin" {
		return
	}
	shown := a.view.SidebarShown()
	if a.menuSet && shown == a.menuSidebar {
		return
	}
	a.menuSet, a.menuSidebar = true, shown
	label := "Show Sidebar"
	if shown {
		label = "Hide Sidebar"
	}
	toggle := func(*mygo.MenuItem, *mygo.Window) { a.post(a.view.ToggleSidebar) }
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: label, Accelerator: "Ctrl+Cmd+S", Click: toggle},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
	}))
}
