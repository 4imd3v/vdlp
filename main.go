// VDLP is a front end for the command line media downloaders: yt-dlp,
// youtube-dl, gallery-dl, lux, you-get, svtplay-dl, aria2c, wget and Safari
// Books, as Media Downloader drives them, with a native interface MyGo
// draws itself in Go.
//
//	go tool mygo dev
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func main() {
	// The window of VDLP is ui.View, so the updater draws its window itself
	// rather than in a web page, which would need a webview VDLP does not
	// otherwise ship.
	mygo.Use(native.Plugin)
	mygo.App.WhenReady(func() {
		set, err := loadSettings()
		if err != nil {
			log.Println("vdlp: loading settings:", err)
		}
		a := newApp(set)
		if err != nil {
			a.log.logger().Error("loading the settings, using the defaults", "err", err)
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "VDLP",
			Width:     1000,
			Height:    720,
			MinWidth:  640,
			MinHeight: 480,
			// Opens where the user left it last time.
			StateKey: "main",
			Content:  ui.View(a.view),
		})
		a.win = win
		apps[win] = a
		setMenu(win)
		// Downloads in the queue are given up as the app quits.
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { owner(win).mgr.stopAll() })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// setMenu gives the app the menus a downloader is expected to have.
func setMenu(win *mygo.Window) {
	goTo := []*mygo.MenuItem{}
	for i, p := range pages {
		goTo = append(goTo, &mygo.MenuItem{
			Label:       p.title,
			Accelerator: "CmdOrCtrl+" + string(rune('1'+i)),
			Click:       func(*mygo.MenuItem, *mygo.Window) { owner(win).goTo(p.id) },
		})
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "New download", Accelerator: "CmdOrCtrl+N", Click: func(*mygo.MenuItem, *mygo.Window) {
				owner(win).goTo("download")
			}},
			{Label: "Import links…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) {
				owner(win).importLinks()
			}},
			mygo.Separator(),
			{Label: "Quit", Role: mygo.RoleQuit},
		}},
		{Label: "Go", Submenu: goTo},
		{Label: "Help", Submenu: []*mygo.MenuItem{
			{Label: "About VDLP", Click: func(*mygo.MenuItem, *mygo.Window) {
				owner(win).goTo("about")
			}},
			updater.MenuItem(),
			{Label: "yt-dlp on GitHub", Click: func(*mygo.MenuItem, *mygo.Window) {
				_ = mygo.Shell.OpenExternal("https://github.com/yt-dlp/yt-dlp")
			}},
		}},
	}))
}

// owner returns the app whose state the window shows.
func owner(win *mygo.Window) *app {
	return apps[win]
}

// apps maps the windows to the app state they show, so the menus can reach
// it. The state itself lives in the window's view.
var apps = map[*mygo.Window]*app{}
