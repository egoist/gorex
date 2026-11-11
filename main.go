// GoRex is a terminal of tabs and split panes, after Superlogical's Rex,
// in MyGo's native UI. Its shells run in a session server of their own,
// so that they outlive the app: quit it, open it again, and every tab and
// pane is back as it was, its programs still running.
//
//	go tool mygo dev    the app, rebuilt as the code changes
//	go run . -server    the session server alone, in the foreground
package main

import (
	"encoding/json"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

func main() {
	// -server runs the session server; other arguments are AppKit's, as
	// -NSWindowResizeTime, which it reads itself.
	if hasFlag("server") {
		log.SetPrefix("[server] ")
		if err := rex.Serve(); err != nil {
			log.Fatal(err)
		}
		return
	}
	// The state of the sessions lives in the app's data directory, which
	// the server it starts takes from GOREX_DIR: "GoRex", or "GoRex Dev"
	// under mygo dev, which keeps the installed app's sessions apart.
	if os.Getenv("GOREX_DIR") == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			os.Setenv("GOREX_DIR", dir)
		}
	}
	// -stop stops the server that runs, ending every program in its
	// sessions, as Rex's "rex server stop" does.
	if hasFlag("stop") {
		os.Exit(stopServer())
	}
	registerFonts()
	loadSettings()
	a := &App{}
	mygo.App.SetMenu(a.menu())
	mygo.App.WhenReady(a.open)
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && a.win == nil {
			a.open()
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			mygo.App.Quit()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.saveNow()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// open opens the window, with the tabs the server kept.
func (a *App) open() {
	client, err := rex.Connect()
	if err != nil {
		log.Print(err)
		mygo.Dialog.Error("GoRex could not start its session server", err.Error())
		mygo.App.Quit()
		return
	}
	a.client = client
	a.hello, err = client.Hello()
	if err == nil && mygo.IsDev() && staleServer(a.hello) {
		// A server a build before this one started runs that build's
		// code: start one of this build's, ending its sessions.
		if client, err = client.Restart(a.hello.PID); err == nil {
			a.client = client
			a.hello, err = client.Hello()
		} else {
			log.Print(err)
			mygo.Dialog.Error("GoRex could not restart its session server", err.Error())
			mygo.App.Quit()
			return
		}
	}
	if err == nil && a.hello.Version != rex.ProtocolVersion {
		a.err = "The session server is of another version of GoRex: quit and end all sessions to restart it."
	}
	a.reset()
	opts := mygo.WindowOptions{
		Title:                "GoRex",
		Width:                1000,
		Height:               620,
		MinWidth:             560,
		MinHeight:            340,
		StateKey:             "main",
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 16, Y: 15},
		BackgroundColor:      "light-dark(#efe1e6, #231e27)",
		Content:              ui.View(a.view),
	}
	if ownControls {
		// The title bar draws the window's controls (windowControls).
		opts.TitleBarStyle, opts.Frameless = mygo.TitleBarDefault, true
	}
	win := mygo.NewWindow(opts)
	a.win = win
	if !a.restore() {
		home, _ := os.UserHomeDir()
		a.newTab(home)
	}
	a.changed()
	go a.poll(win, client)
	a.debugHook()
	win.OnClosed(func() {
		a.saveNow()
		a.quitting = true
		for _, t := range a.tabs {
			for _, p := range t.panes() {
				p.closed = true
				if p.term != nil {
					p.term.Close() // detaches: the session goes on
				}
			}
		}
		a.client.Close()
		a.win = nil
	})
}

// saveNow sends the layout to the server and waits for it to be kept.
func (a *App) saveNow() {
	if a.client == nil || len(a.tabs) == 0 && !a.quitting {
		return
	}
	b, err := json.Marshal(a.snapshot())
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		a.client.SetLayout(b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}

// reset forgets the state of a window that closed, keeping the server's
// connection, for the next window.
func (a *App) reset() {
	a.tabs, a.active, a.focusReq = nil, 0, nil
	a.paletteOpen, a.hostOpen, a.renaming = false, false, nil
	a.saveDue, a.quitting, a.lastSnapshot, a.title = false, false, "", ""
}

// staleServer reports whether the server runs another build of the app
// than this one, as after mygo dev rebuilt it.
func staleServer(h rex.Hello) bool {
	exe, at := rex.Executable()
	return h.Exe != exe || !h.ExeTime.Equal(at)
}

// stopServer stops the server and waits for it to go, returning the exit
// status: 4, as Rex's, when no server runs.
func stopServer() int {
	c, err := rex.Dial()
	if err != nil {
		log.Print("rex: no server runs")
		return 4
	}
	if err := c.Shutdown(); err != nil {
		log.Print(err)
		return 1
	}
	c.Close()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(30 * time.Millisecond) {
		c, err := rex.Dial()
		if err != nil {
			return 0
		}
		c.Close()
	}
	log.Print("rex: the server did not stop")
	return 1
}

// hasFlag reports whether the first argument is -name or --name. The
// flag package isn't used, as it would reject AppKit's arguments.
func hasFlag(name string) bool {
	return len(os.Args) > 1 && (os.Args[1] == "-"+name || os.Args[1] == "--"+name)
}
