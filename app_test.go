package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gorex/internal/rex"
)

// testShell is the shell of the tests: its prompt ends with prompt, and
// typing run runs program for a while in dir.
var testShell = func() (s struct{ path, prompt, run, program, dir string }) {
	if runtime.GOOS == "windows" {
		s.path, s.prompt, s.program, s.dir = "cmd.exe", ">", "PING", os.Getenv("SystemRoot")
		s.run = `cd /d ` + s.dir + ` && ping -n 4 127.0.0.1 >nul`
		return
	}
	s.path, s.prompt, s.run, s.program, s.dir = "/bin/sh", "$", "cd /usr/bin && sleep 3", "sleep", "/usr/bin"
	return
}()

// testDir is a directory of the tests' own: in /tmp, whose path is short
// enough for the socket, on macOS.
func testDir(t *testing.T) string {
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = ""
	}
	dir, err := os.MkdirTemp(base, "gorex")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// newTestApp starts a session server of its own and an app on it, whose
// view runs without a window.
func newTestApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	dir := testDir(t)
	t.Setenv("GOREX_DIR", dir)
	t.Setenv("SHELL", testShell.path)
	go rex.Serve()
	var client *rex.Client
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(rex.SocketPath()); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	client, err := rex.Connect()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Shutdown()
		time.Sleep(100 * time.Millisecond)
		os.RemoveAll(dir)
	})
	registerFonts()
	a := &App{client: client}
	a.newTab(os.TempDir())
	tt := ui.NewTester(a.view, 1000, 620)
	return a, tt
}

// refresh asks the server what runs in the sessions, as the window does
// every half second.
func refresh(a *App) {
	infos, _ := a.client.List()
	byID := map[string]rex.SessionInfo{}
	for _, in := range infos {
		byID[in.ID] = in
	}
	a.apply(byID)
}

// waitFor runs frames until cond holds, or fails after a few seconds.
func waitFor(t *testing.T, tt *ui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; texts %q", what, tt.Texts())
		}
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
}

func TestPanesAndTabs(t *testing.T) {
	a, tt := newTestApp(t)
	tab := a.tab()
	p := tab.Focus
	waitFor(t, tt, "the shell", func() bool { return p.term != nil && strings.Contains(p.term.Text(), testShell.prompt) })

	// The command palette splits the pane.
	a.openPalette()
	tt.Frame()
	tt.Type("split right")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if n := len(tab.panes()); n != 2 {
		t.Fatalf("%d panes after Split Right", n)
	}
	if a.paletteOpen {
		t.Error("the palette stays open")
	}
	right := tab.Focus
	if right == p {
		t.Fatal("the new pane does not have the focus")
	}

	// The header's buttons split down and zoom.
	if err := tt.Click("Split Down"); err != nil {
		t.Fatal(err)
	}
	if n := len(tab.panes()); n != 3 || !tab.Root.B.Vertical {
		t.Fatalf("%d panes after Split Down", n)
	}
	if err := tt.Click("Zoom"); err != nil {
		t.Fatal(err)
	}
	if tab.Zoom == nil {
		t.Fatal("not zoomed")
	}
	a.toggleZoom()

	// Focus moves between panes by where they are: the frame after the one
	// that lays them out anew knows where.
	tt.Frame()
	tt.Frame()
	a.moveFocus(-1, 0)
	if tab.Focus != p {
		t.Errorf("focus left went to pane %d, not %d", tab.Focus.ID, p.ID)
	}
	a.moveFocus(1, 0)
	if tab.Focus == p {
		t.Error("focus right stayed")
	}

	// Typing reaches the shell, and the header follows its directory.
	a.focusReq = p
	tab.setFocus(p)
	tt.Frame()
	tt.Type(testShell.run)
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, testShell.program+" in the header", func() bool {
		refresh(a)
		name, detail := p.label()
		return strings.EqualFold(name, testShell.program) && samePath(detail, shortDir(testShell.dir))
	})

	// A second tab, renamed, then closed with its pane.
	a.newTab(os.TempDir())
	if len(a.tabs) != 2 || a.active != 1 {
		t.Fatalf("%d tabs, active %d", len(a.tabs), a.active)
	}
	a.startRename()
	tt.Frame()
	tt.Type("logs")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.tabs[1].Name != "logs" {
		t.Errorf("tab named %q", a.tabs[1].Name)
	}
	a.closeFocused()
	if len(a.tabs) != 1 || a.active != 0 {
		t.Fatalf("%d tabs, active %d, after closing the last pane of a tab", len(a.tabs), a.active)
	}

	// A shell that exits closes its pane.
	a.selectTab(0)
	tab.setFocus(right)
	right.term.Send([]byte("exit\r"))
	waitFor(t, tt, "the pane to close", func() bool { return len(tab.panes()) == 2 })
}

func TestRestore(t *testing.T) {
	a, tt := newTestApp(t)
	a.split(false)
	a.tab().Root.Ratio = 0.3
	a.newTab(testShell.dir)
	a.tabs[1].Name = "second"
	tt.Frame()
	a.saveNow()
	sids := map[string]bool{}
	for _, tab := range a.tabs {
		for _, p := range tab.panes() {
			sids[p.SID] = true
			p.term.Close() // detach, as when the app quits
		}
	}
	// Another app, as the next launch, finds them all.
	b := &App{client: a.client}
	if !b.restore() {
		t.Fatal("nothing restored")
	}
	if len(b.tabs) != 2 || b.tabs[1].Name != "second" || b.active != 1 {
		t.Fatalf("restored %d tabs, active %d", len(b.tabs), b.active)
	}
	if r := b.tabs[0].Root; r.Pane != nil || r.Ratio != 0.3 {
		t.Errorf("first tab's root %+v", r)
	}
	for _, tab := range b.tabs {
		for _, p := range tab.panes() {
			if !sids[p.SID] || p.restored {
				t.Errorf("pane of session %q not attached again", p.SID)
			}
		}
	}
}

func TestLabels(t *testing.T) {
	home, _ := os.UserHomeDir()
	site, short := filepath.Join(home, "Sites", "rex-snake"), filepath.Join("~", "Sites", "rex-snake")
	p := &Pane{info: rex.SessionInfo{Shell: "zsh", Program: "zsh", Idle: true, Dir: site}}
	if n, d := p.label(); n != "zsh" || d != short {
		t.Errorf("shell label %q %q", n, d)
	}
	p.info = rex.SessionInfo{Shell: "fish", Program: "lazygit", Dir: site}
	p.title = "lazygit ~/Sites/rex-snake"
	if n, d := p.label(); n != "Git Changes" || d != short {
		t.Errorf("lazygit label %q %q", n, d)
	}
	p.info.Program = "codex"
	p.title = "Stress-test Snake demo | rex-snake"
	if n, d := p.label(); n != p.title || d != "" {
		t.Errorf("codex label %q %q", n, d)
	}
	p.info, p.title = rex.SessionInfo{Shell: "zsh", Program: "ssh", Args: []string{"ssh", "-p", "22", "box.local"}}, ""
	if n, d := p.label(); n != "SSH" || d != "box.local" {
		t.Errorf("ssh label %q %q", n, d)
	}
	// Windows's consoles name the shell in their title as a program runs.
	p.info, p.title = rex.SessionInfo{Shell: "cmd", Program: "node", Dir: site}, `C:\WINDOWS\system32\cmd.exe - node app.js`
	if n, d := p.label(); n != "Node" || d != short {
		t.Errorf("node label %q %q", n, d)
	}
	if d := shortDir(filepath.FromSlash("/private/tmp/a/b/c/d/e")); d != filepath.FromSlash("…/d/e") {
		t.Errorf("short dir %q", d)
	}
}

// TestCloseButtons presses and releases the close buttons that show while
// the pointer is over a tab or a pane, with a frame between: pressing one
// must not hide it.
func TestCloseButtons(t *testing.T) {
	a, tt := newTestApp(t)
	first := a.tabs[0]
	a.newTab(os.TempDir())
	tt.Frame()
	track, ok := tt.Find("Tabs")
	if !ok {
		t.Fatal("no tab bar")
	}
	// The pointer over the first tab shows its close button.
	tt.Move(track.X+40, track.Y+track.H/2)
	tt.Frame()
	x, ok := tt.Find("Close Tab")
	if !ok {
		t.Fatalf("no close button over the tab; texts %q", tt.Texts())
	}
	tt.Press(x.X+x.W/2, x.Y+x.H/2)
	tt.Frame()
	tt.Release(x.X+x.W/2, x.Y+x.H/2)
	tt.Frame()
	if len(a.tabs) != 1 || slices.Contains(a.tabs, first) {
		t.Fatalf("%d tabs after clicking the first one's close button", len(a.tabs))
	}

	// A pane without the focus shows its buttons under the pointer.
	a.split(false)
	tt.Frame()
	tab := a.tab()
	left := tab.panes()[0]
	if tab.Focus == left {
		t.Fatal("the left pane has the focus")
	}
	b := left.bounds
	tt.Move(b.X+60, b.Y+headerH/2)
	tt.Frame()
	cx, cy := b.X+b.W-8-13, b.Y+headerH/2 // the last button of the header
	tt.Press(cx, cy)
	tt.Frame()
	tt.Release(cx, cy)
	tt.Frame()
	if ps := tab.panes(); len(ps) != 1 || ps[0] == left {
		t.Fatalf("%d panes after clicking the left pane's close button", len(ps))
	}
}

// TestPaletteScrolls checks that the wheel scrolls a palette longer than
// it shows, and that the keys bring the selection back into view.
func TestPaletteScrolls(t *testing.T) {
	registerFonts()
	a := &App{}
	tab := &Tab{ID: 1}
	for i := range 20 {
		n := &Node{Pane: &Pane{ID: i, Tab: tab, info: rex.SessionInfo{Shell: fmt.Sprintf("shell%02d", i)}}}
		n.Pane.Node = n
		if tab.Root == nil {
			tab.Root = n
		} else {
			tab.Root = &Node{A: tab.Root, B: n, Ratio: 0.5}
		}
	}
	a.tabs = []*Tab{tab}
	tt := ui.NewTester(func(c *ui.Context) { a.palette(c, colorsOf(c)) }, 1000, 620)
	a.openPalette()
	tt.Frame()
	// A row out of sight finds an empty box.
	top, _ := tt.Find("shell00")
	row10, _ := tt.Find("shell10")
	tt.Move(top.X, top.Y+100)
	tt.Scroll(top.X, top.Y+100, 0, 300)
	tt.Frame()
	if r, _ := tt.Find("shell10"); r.Y != row10.Y-300 {
		t.Errorf("scrolled 300: a row moved from %v to %v", row10.Y, r.Y)
	}
	if r, _ := tt.Find("shell00"); r.H != 0 {
		t.Errorf("scrolled 300: the first row still shows, at %v", r.Y)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if r, _ := tt.Find("shell01"); r.H == 0 {
		t.Error("the keys chose a row out of sight")
	}
}
