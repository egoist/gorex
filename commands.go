package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// command is an action of the menus and the command palette.
type command struct {
	Title string
	// Accel is the menu item's shortcut, and Keys how the palette shows it.
	Accel, Keys string
	Run         func(a *App)
	// Hidden keeps it out of the palette.
	Hidden bool
}

var (
	cmdNewTab      = command{Title: "New Tab", Accel: "CmdOrCtrl+T", Keys: "⌘T", Run: func(a *App) { a.newTab(a.currentDir()) }}
	cmdSplitRight  = command{Title: "Split Right", Accel: "CmdOrCtrl+D", Keys: "⌘D", Run: func(a *App) { a.split(false) }}
	cmdSplitDown   = command{Title: "Split Down", Accel: "CmdOrCtrl+Shift+D", Keys: "⇧⌘D", Run: func(a *App) { a.split(true) }}
	cmdClosePane   = command{Title: "Close Pane", Accel: "CmdOrCtrl+W", Keys: "⌘W", Run: func(a *App) { a.closeFocused() }}
	cmdCloseTab    = command{Title: "Close Tab", Accel: "CmdOrCtrl+Shift+W", Keys: "⇧⌘W", Run: func(a *App) { a.closeActiveTab() }}
	cmdZoom        = command{Title: "Zoom Pane", Accel: "CmdOrCtrl+Shift+Enter", Keys: "⇧⌘↩", Run: func(a *App) { a.toggleZoom() }}
	cmdEqualize    = command{Title: "Equalize Panes", Accel: "CmdOrCtrl+Ctrl+=", Keys: "⌃⌘=", Run: func(a *App) { a.equalize() }}
	cmdNextTab     = command{Title: "Next Tab", Accel: "CmdOrCtrl+Shift+]", Keys: "⇧⌘]", Run: func(a *App) { a.cycleTab(1) }}
	cmdPrevTab     = command{Title: "Previous Tab", Accel: "CmdOrCtrl+Shift+[", Keys: "⇧⌘[", Run: func(a *App) { a.cycleTab(-1) }}
	cmdRenameTab   = command{Title: "Rename Tab…", Accel: "CmdOrCtrl+Shift+R", Keys: "⇧⌘R", Run: func(a *App) { a.startRename() }}
	cmdPalette     = command{Title: "Command Palette…", Accel: "CmdOrCtrl+Shift+P", Keys: "⇧⌘P", Run: func(a *App) { a.openPalette() }, Hidden: true}
	cmdPalette2    = command{Title: "Go to Pane…", Accel: "CmdOrCtrl+P", Keys: "⌘P", Run: func(a *App) { a.openPalette() }, Hidden: true}
	cmdFocusLeft   = command{Title: "Focus Pane Left", Accel: "CmdOrCtrl+Alt+Left", Keys: "⌥⌘←", Run: func(a *App) { a.moveFocus(-1, 0) }}
	cmdFocusRight  = command{Title: "Focus Pane Right", Accel: "CmdOrCtrl+Alt+Right", Keys: "⌥⌘→", Run: func(a *App) { a.moveFocus(1, 0) }}
	cmdFocusUp     = command{Title: "Focus Pane Above", Accel: "CmdOrCtrl+Alt+Up", Keys: "⌥⌘↑", Run: func(a *App) { a.moveFocus(0, -1) }}
	cmdFocusDown   = command{Title: "Focus Pane Below", Accel: "CmdOrCtrl+Alt+Down", Keys: "⌥⌘↓", Run: func(a *App) { a.moveFocus(0, 1) }}
	cmdGrowLeft    = command{Title: "Move Divider Left", Accel: "CmdOrCtrl+Ctrl+Left", Keys: "⌃⌘←", Run: func(a *App) { a.resizeFocused(-1, 0) }}
	cmdGrowRight   = command{Title: "Move Divider Right", Accel: "CmdOrCtrl+Ctrl+Right", Keys: "⌃⌘→", Run: func(a *App) { a.resizeFocused(1, 0) }}
	cmdGrowUp      = command{Title: "Move Divider Up", Accel: "CmdOrCtrl+Ctrl+Up", Keys: "⌃⌘↑", Run: func(a *App) { a.resizeFocused(0, -1) }}
	cmdGrowDown    = command{Title: "Move Divider Down", Accel: "CmdOrCtrl+Ctrl+Down", Keys: "⌃⌘↓", Run: func(a *App) { a.resizeFocused(0, 1) }}
	cmdClear       = command{Title: "Clear Screen and Scrollback", Accel: "CmdOrCtrl+K", Keys: "⌘K", Run: func(a *App) { a.clearFocused() }, Hidden: true}
	cmdClearScroll = command{Title: "Clear Scrollback", Run: func(a *App) { a.clearFocused() }}
	cmdRestart     = command{Title: "Restart Shell in Pane", Run: func(a *App) { a.restartFocused() }}
	cmdBigger      = command{Title: "Bigger Text", Accel: "CmdOrCtrl+=", Keys: "⌘+", Run: func(a *App) { a.setFontSize(termFont.Size + 1) }}
	cmdSmaller     = command{Title: "Smaller Text", Accel: "CmdOrCtrl+-", Keys: "⌘−", Run: func(a *App) { a.setFontSize(termFont.Size - 1) }}
	cmdActualSize  = command{Title: "Actual Size", Accel: "CmdOrCtrl+0", Keys: "⌘0", Run: func(a *App) { a.setFontSize(defaultFontSize) }}
	cmdLight       = command{Title: "Appearance: Light", Run: func(a *App) { a.setAppearance("light") }}
	cmdDark        = command{Title: "Appearance: Dark", Run: func(a *App) { a.setAppearance("dark") }}
	cmdSystem      = command{Title: "Appearance: System", Run: func(a *App) { a.setAppearance("") }}
	cmdEndAll      = command{Title: "Quit and End All Sessions", Accel: "CmdOrCtrl+Alt+Q", Keys: "⌥⌘Q", Run: func(a *App) { a.quitAndEnd() }}
)

// paletteCommands are those the command palette lists, before the tabs.
var paletteCommands = []*command{
	&cmdNewTab, &cmdSplitRight, &cmdSplitDown, &cmdZoom, &cmdEqualize, &cmdClosePane, &cmdCloseTab,
	&cmdNextTab, &cmdPrevTab, &cmdRenameTab,
	&cmdFocusLeft, &cmdFocusRight, &cmdFocusUp, &cmdFocusDown,
	&cmdGrowLeft, &cmdGrowRight, &cmdGrowUp, &cmdGrowDown,
	&cmdBigger, &cmdSmaller, &cmdActualSize, &cmdLight, &cmdDark, &cmdSystem,
	&cmdClearScroll, &cmdRestart, &cmdEndAll,
}

// menu builds the menu bar, whose items run commands in the window.
func (a *App) menu() *mygo.Menu {
	item := func(cmd *command) *mygo.MenuItem {
		return &mygo.MenuItem{Label: cmd.Title, Accelerator: cmd.Accel, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.run(cmd)
		}}
	}
	tabItems := []*mygo.MenuItem{item(&cmdNextTab), item(&cmdPrevTab), mygo.Separator()}
	for i := 1; i <= 9; i++ {
		i := i
		tabItems = append(tabItems, &mygo.MenuItem{
			Label: fmt.Sprintf("Show Tab %d", i), Accelerator: fmt.Sprintf("CmdOrCtrl+%d", i),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.do(func() {
					if i == 9 {
						a.selectTab(len(a.tabs) - 1)
					} else {
						a.selectTab(i - 1)
					}
				})
			},
		})
	}
	appearance := func(label, v string) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Type: mygo.MenuItemRadio, Checked: prefs.Appearance == v, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.setAppearance(v)
		}}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "Shell", Submenu: []*mygo.MenuItem{
			item(&cmdNewTab),
			mygo.Separator(),
			item(&cmdSplitRight),
			item(&cmdSplitDown),
			mygo.Separator(),
			item(&cmdClosePane),
			item(&cmdCloseTab),
			mygo.Separator(),
			item(&cmdRestart),
			item(&cmdEndAll),
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleUndo},
			{Role: mygo.RoleRedo},
			mygo.Separator(),
			{Role: mygo.RoleCut},
			{Role: mygo.RoleCopy},
			{Role: mygo.RolePaste},
			{Role: mygo.RoleSelectAll},
			mygo.Separator(),
			item(&cmdClear),
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			item(&cmdPalette),
			item(&cmdPalette2),
			mygo.Separator(),
			item(&cmdZoom),
			item(&cmdEqualize),
			mygo.Separator(),
			{Label: "Focus", Submenu: []*mygo.MenuItem{item(&cmdFocusLeft), item(&cmdFocusRight), item(&cmdFocusUp), item(&cmdFocusDown)}},
			{Label: "Resize", Submenu: []*mygo.MenuItem{item(&cmdGrowLeft), item(&cmdGrowRight), item(&cmdGrowUp), item(&cmdGrowDown)}},
			mygo.Separator(),
			item(&cmdBigger),
			item(&cmdSmaller),
			item(&cmdActualSize),
			{Label: "Appearance", Submenu: []*mygo.MenuItem{
				appearance("System", ""),
				appearance("Light", "light"),
				appearance("Dark", "dark"),
			}},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
			{Role: mygo.RoleToggleDevTools, Label: "Inspector", Hidden: !mygo.IsDev()},
		}},
		{Label: "Tabs", Submenu: append(tabItems, mygo.Separator(), item(&cmdRenameTab))},
		{Role: mygo.RoleWindowMenu},
	})
}

// do changes the state on the main thread, from a menu, and draws.
func (a *App) do(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(fn)
}

// run runs a command of the menus, on the main thread. New Tab opens the
// window again when it was closed.
func (a *App) run(cmd *command) {
	if a.win == nil {
		if cmd == &cmdNewTab {
			mygo.RunOnMain(a.open)
		}
		return
	}
	a.do(func() {
		if a.win != nil {
			cmd.Run(a)
		}
	})
}

func (a *App) closeFocused() {
	if t := a.tab(); t != nil && t.Focus != nil {
		a.closePane(t.Focus)
	}
}

func (a *App) closeActiveTab() {
	if t := a.tab(); t != nil {
		a.closeTab(t)
	}
}

func (a *App) cycleTab(d int) {
	if n := len(a.tabs); n > 1 {
		a.selectTab(((a.active+d)%n + n) % n)
	}
}

func (a *App) startRename() {
	if t := a.tab(); t != nil {
		a.renaming, a.renameText, a.renameFocus = t, t.Name, false
		a.focusReq = nil // the field takes the focus
	}
}

func (a *App) clearFocused() {
	if t := a.tab(); t != nil && t.Focus != nil && t.Focus.term != nil {
		// Clear the scrollback and the screen, and have the shell draw
		// its prompt again, as Command-K does in Terminal.
		t.Focus.term.Feed([]byte("\x1b[H\x1b[2J\x1b[3J"))
		if t.Focus.info.Idle {
			t.Focus.term.Send([]byte{0x0c})
		}
	}
}

// restartFocused ends the focused pane's session and starts a shell in
// its place, in the same directory.
func (a *App) restartFocused() {
	t := a.tab()
	if t == nil || t.Focus == nil {
		return
	}
	old := t.Focus
	cols, rows := 80, 24
	if old.term != nil {
		cols, rows = old.term.Size()
	}
	p := a.newPane(a.currentDir(), cols, rows)
	p.Tab, p.Node = t, old.Node
	old.Node.Pane = p
	old.closed = true
	if old.term != nil {
		old.term.Close()
	}
	go a.client.Kill(old.SID)
	if t.Zoom == old {
		t.Zoom = p
	}
	t.Focus = nil
	t.setFocus(p)
	a.focusReq = p
	a.changed()
}

func (a *App) quitAndEnd() {
	a.quitting = true
	for _, t := range a.tabs {
		for _, p := range t.panes() {
			p.closed = true
		}
	}
	c := a.client
	go func() {
		c.Shutdown()
		mygo.App.Quit()
	}()
}

func (a *App) openPalette() {
	a.paletteOpen, a.paletteQuery, a.paletteSel = true, "", 0
	a.focusReq = nil // the palette takes the focus
}

// shortcuts handles the keys the menus do not: Escape out of a zoomed
// pane, and Control-Tab between tabs.
func (a *App) shortcuts(c *ui.Context) {
	if c.Shortcut(ui.Ctrl, ui.KeyTab) {
		a.cycleTab(1)
	}
	if c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab) {
		a.cycleTab(-1)
	}
}

// paletteItem is a row of the command palette: a command, or a pane to
// go to.
type paletteItem struct {
	title, detail, keys string
	glyph               string
	run                 func()
}

func (a *App) paletteItems() []paletteItem {
	var items []paletteItem
	q := strings.ToLower(strings.TrimSpace(a.paletteQuery))
	match := func(s string) bool { return q == "" || fuzzy(strings.ToLower(s), q) }
	for ti, t := range a.tabs {
		for _, p := range t.panes() {
			name, detail := p.label()
			if !match(fmt.Sprintf("%s %s tab %d", name, detail, ti+1)) {
				continue
			}
			ti, t, p := ti, t, p
			items = append(items, paletteItem{
				title: name, detail: detail, glyph: paneProgram(p).Glyph,
				keys: fmt.Sprintf("Tab %d", ti+1),
				run: func() {
					a.selectTab(ti)
					t.setFocus(p)
					a.focusReq = p
				},
			})
		}
	}
	for _, cmd := range paletteCommands {
		if cmd.Hidden || !match(cmd.Title) {
			continue
		}
		cmd := cmd
		items = append(items, paletteItem{title: cmd.Title, keys: cmd.Keys, glyph: "command", run: func() { cmd.Run(a) }})
	}
	return items
}

// fuzzy reports whether the letters of q appear in s, in order.
func fuzzy(s, q string) bool {
	if strings.Contains(s, q) {
		return true
	}
	i := 0
	for _, r := range s {
		if i < len(q) && byte(r) == q[i] {
			i++
		}
	}
	return i == len(q)
}

// palette draws the command palette, over the window.
func (a *App) palette(c *ui.Context, k *colors) {
	if !a.paletteOpen {
		return
	}
	items := a.paletteItems()
	a.paletteSel = min(max(a.paletteSel, 0), max(len(items)-1, 0))
	was := a.paletteOpen
	ui.DialogBase(c, &a.paletteOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(k.backdrop).Justify(ui.Start).Padding(78, 0, 0, 0)
		panel.Width(560).MaxHeight(440).Radius(16).Background(k.panel).Border(1, k.panelBorder).
			Shadow(0, 24, 60, -8, ui.RGBA(0, 0, 0, 0.28)).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.06)).Clip()
		ui.Row(c).Padding(12, 16).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(k.panelBorder).Children(func() {
			ui.Icon(c, icon("search")).Size(17, 17).TextColor(k.textFaint)
			in := ui.TextInputBase(c, &a.paletteQuery).Grow(1).FontSize(15).Placeholder("Type a command, or the name of a pane…").AutoFocus()
			if in.Changed() {
				a.paletteSel = 0
			}
			if in.Shortcut(0, ui.KeyDown) {
				a.paletteSel = min(a.paletteSel+1, len(items)-1)
			}
			if in.Shortcut(0, ui.KeyUp) {
				a.paletteSel = max(a.paletteSel-1, 0)
			}
			if in.Submitted() && len(items) > 0 {
				a.paletteOpen = false
				items[a.paletteSel].run()
			}
		})
		listH := float32(min(max(len(items), 1)*34+12, 384))
		ui.Scroll(c).Height(listH).Padding(6).Children(func() {
			if len(items) == 0 {
				ui.Text(c, "No matches").FontSize(13).TextColor(k.textFaint).Padding(14)
			}
			for i, it := range items {
				row := ui.Row(c.Key(i)).Height(34).Padding(0, 10).Gap(10).Radius(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				if i == a.paletteSel {
					row.Background(k.panelSel)
					row.ScrollIntoView()
				} else if row.Hovered() {
					row.Background(k.hover)
				}
				if row.Clicked() {
					a.paletteOpen = false
					it.run()
				}
				row.Children(func() {
					ui.Icon(c, icon(it.glyph)).Size(15, 15).TextColor(k.textMuted)
					ui.Text(c, it.title).FontSize(13.5).FontWeight(500).TextColor(k.text).SingleLine().Ellipsis("…").Shrink(0.3).MinWidth(0)
					if it.detail != "" {
						ui.Text(c, it.detail).FontSize(13).TextColor(k.textFaint).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
					}
					ui.Spacer(c)
					if it.keys != "" {
						ui.Text(c, it.keys).FontSize(12).TextColor(k.textFaint).Padding(2, 7).Radius(6).Background(k.hover).Shrink(0)
					}
				})
			}
		})
	})
	if was && !a.paletteOpen {
		if t := a.tab(); t != nil && a.focusReq == nil {
			a.focusReq = t.Focus
		}
	}
}
