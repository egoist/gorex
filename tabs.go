package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

const (
	tabH     = 28
	tabMaxW  = 214
	tabMinW  = 120
	tileW    = 21
	tileH    = 16
	tileStep = 5 // how far each tile behind peeks out
)

// tabStrip draws the tabs, in a track as wide as they are, which shrinks
// with them when the title bar has no room for them all. The track is part
// of the title bar: its edges and the gaps between tabs drag the window.
func (a *App) tabStrip(c *ui.Context, k *colors) {
	widths := make([]float32, len(a.tabs))
	total := float32(4 + max(len(a.tabs)-1, 0)) // the padding and separators
	for i, t := range a.tabs {
		widths[i] = tabWidth(c, t)
		total += widths[i]
	}
	track := ui.Row(c).Basis(total).Shrink(1).MinWidth(0).Height(tabH+4).Padding(2).Radius((tabH+4)/2).
		Background(k.track).Border(0.5, k.trackBorder).AlignItems(ui.Center).ClipX().
		DragWindow().Role(ui.RoleTabList).Label("Tabs")
	tabs := slices.Clone(a.tabs)
	track.Children(func() {
		for i, t := range tabs {
			if i > 0 {
				sep := ui.Box(c).Size(1, 16).Shrink(0)
				if i != a.active && i-1 != a.active {
					sep.Background(k.tabSep)
				}
			}
			a.tabItem(c, k, i, t, widths[i])
		}
	})
}

// tabWidth returns the width a tab takes when the title bar has room: its
// tiles and its label, the label faded out past tabMaxW, and room for its
// close button or its dot.
func tabWidth(c *ui.Context, t *Tab) float32 {
	name, detail := t.label()
	w, _ := c.MeasureText(0, tabLabel(name, detail, ui.Color{}, ui.Color{})...)
	tiles := float32(tileW + tileStep*min(len(t.panes())-1, 2) + 2)
	return min(max(6+tiles+9+w+9+18+10, tabMinW), tabMaxW)
}

// tabLabel is the label of a tab: the name and, fainter, the detail.
func tabLabel(name, detail string, nameColor, detailColor ui.Color) []ui.Span {
	return []ui.Span{
		{Text: name, Size: 13.5, Weight: 500, Color: nameColor},
		{Text: " " + detail, Size: 13.5, Weight: 400, Color: detailColor},
	}
}

// titleFree is how much of the title bar the tabs leave free, after
// them, to drag the window by.
const titleFree = 56

// tabItem draws a tab: the tiles of its panes' programs and its label.
func (a *App) tabItem(c *ui.Context, k *colors, i int, t *Tab, width float32) {
	active := i == a.active
	e := ui.Row(c.Key(t.ID)).Height(tabH).Basis(width).Shrink(1).MinWidth(64).
		Padding(0, 10, 0, 6).Gap(9).AlignItems(ui.Center).Radius(tabH / 2).Role(ui.RoleTab).Selected(active)
	name, detail := t.label()
	e.Label(name + " " + detail)
	if active {
		e.Background(k.tabActive).Shadow(0, 1, 2, 0, k.shadow).Shadow(0, 2, 8, 0, k.shadow)
	} else if e.Hovered() {
		e.Background(k.hover)
	}
	e.Transition(ui.ElementTransition{Colors: true, Position: true, Duration: 160 * time.Millisecond})
	e.Drag(t)
	if dragged, ok := ui.Drop[*Tab](e); ok && dragged != t {
		a.moveTab(dragged, i)
	}
	if _, over := ui.DragOver[*Tab](e); over {
		e.Border(1.5, k.busy.Alpha(0.6))
	}
	if e.Dragging() {
		e.Opacity(0.4)
	}
	if e.Clicked() && !active {
		a.selectTab(i)
	}
	if e.DoubleClicked() {
		a.selectTab(i)
		a.startRename()
	}
	e.ContextMenu(func(m *ui.Menu) { a.tabMenu(m, t) })
	// The tab stays hovered while its close button is pressed, which it
	// shows then.
	hovered := e.Hovered()
	e.Children(func() {
		a.tiles(c, k, t)
		if a.renaming == t {
			a.renameField(c, t, name)
			return
		}
		nameColor, detailColor := k.text, k.textFaint
		if !active {
			nameColor = k.textMuted
		}
		label := ui.Box(c).Grow(1).MinWidth(0).Height(18)
		label.Draw(func(p *ui.Painter, r ui.Rect) {
			fadeText(p, r, tabLabel(name, detail, nameColor, detailColor))
		})
		if hovered && len(a.tabs) > 1 {
			x := iconButton(c, k, "x", "Close Tab", 18, 12).Tooltip("Close Tab")
			if x.Clicked() {
				a.later(c, func() { a.closeTab(t) })
			}
		} else if t.attention() {
			ui.Box(c).Size(7, 7).Radius(4).Background(k.attention).Margin(0, 5, 0, 0)
		} else if a.tabBusy(c, t) {
			activityDot(c, k.busy, 5.5)
		}
	})
}

// renameField edits the name of a tab in place: Enter or moving the focus
// away keeps it, Escape goes back, and an empty name follows the panes
// again.
func (a *App) renameField(c *ui.Context, t *Tab, name string) {
	in := ui.TextInput(c, &a.renameText).Placeholder(name).Grow(1).MinWidth(0).Height(22).FontSize(13).AutoFocus()
	done := func(keep bool) {
		if keep {
			t.Name = strings.TrimSpace(a.renameText)
			a.changed()
		}
		a.renaming, a.renameFocus = nil, false
		a.focusReq = t.Focus
	}
	switch {
	case in.Shortcut(0, ui.KeyEscape):
		done(false)
	case in.Submitted():
		done(true)
	case in.Focused():
		a.renameFocus = true
	case a.renameFocus:
		done(true) // the focus went elsewhere
	}
}

func (t *Tab) attention() bool {
	for _, p := range t.panes() {
		if p.attention {
			return true
		}
	}
	return false
}

func (a *App) tabBusy(c *ui.Context, t *Tab) bool {
	for _, p := range t.panes() {
		if a.statusOf(c, p) == statusRunning {
			return true
		}
	}
	return false
}

func (a *App) moveTab(t *Tab, to int) {
	from := slices.Index(a.tabs, t)
	if from < 0 || to == from {
		return
	}
	activeTab := a.tab()
	a.tabs = slices.Delete(a.tabs, from, from+1)
	a.tabs = slices.Insert(a.tabs, min(to, len(a.tabs)), t)
	a.active = slices.Index(a.tabs, activeTab)
	a.changed()
}

func (a *App) tabMenu(m *ui.Menu, t *Tab) {
	if m.Item("Rename Tab…").Chosen() {
		a.selectTab(slices.Index(a.tabs, t))
		a.startRename()
	}
	if t.Name != "" && m.Item("Reset Name").Chosen() {
		t.Name = ""
		a.changed()
	}
	m.Separator()
	i := slices.Index(a.tabs, t)
	if m.Item("Move Left").Disabled(i <= 0).Chosen() {
		a.moveTab(t, i-1)
	}
	if m.Item("Move Right").Disabled(i >= len(a.tabs)-1).Chosen() {
		a.moveTab(t, i+1)
	}
	m.Separator()
	if m.Item("New Tab").Shortcut(ui.Cmd, ui.KeyT).Chosen() {
		a.newTab(a.currentDir())
	}
	if m.Item("Close Tab").Chosen() {
		a.laterService(func() { a.closeTab(t) })
	}
	if m.Item("Close Other Tabs").Disabled(len(a.tabs) < 2).Chosen() {
		a.laterService(func() {
			for _, o := range slices.Clone(a.tabs) {
				if o != t {
					a.closeTab(o)
				}
			}
		})
	}
}

// tiles draws the tiles of a tab's programs: the focused pane's in front,
// the others peeking out behind it.
func (a *App) tiles(c *ui.Context, k *colors, t *Tab) {
	panes := t.panes()
	front := t.Focus
	var back []*Pane
	for _, p := range panes {
		if p != front {
			back = append(back, p)
		}
	}
	if len(back) > 2 {
		back = back[:2]
	}
	w := float32(tileW + tileStep*len(back) + 2)
	ui.Box(c).Size(w, tileH+4).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		y := r.Y + 2
		for i := len(back) - 1; i >= 0; i-- {
			x := r.X + 1 + float32(tileStep*(i+1))
			inset := float32(i+1) * 0.8
			drawTile(p, k, ui.Rect{X: x, Y: y + inset, W: tileW, H: tileH - 2*inset}, paneProgram(back[i]), false)
		}
		if front != nil {
			drawTile(p, k, ui.Rect{X: r.X + 1, Y: y, W: tileW, H: tileH}, paneProgram(front), true)
		}
	})
}

func paneProgram(p *Pane) program {
	if p.info.Idle || p.info.Program == "" {
		return programOf(p.info.Shell)
	}
	return programOf(p.info.Program)
}

// drawTile draws a program's tile: a rounded card of its color with a
// light rim, and its glyph when it is in front.
func drawTile(p *ui.Painter, k *colors, r ui.Rect, prog program, glyph bool) {
	const rad = 4.5
	p.Shadow(r, rad, 0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.18))
	rim := ui.Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}
	p.Fill(rim, k.tileRim, rad+1)
	p.Fill(r, prog.TileBg, rad)
	if prog.TileBorder {
		p.Stroke(r, ui.RGBA(0, 0, 0, 0.12), rad, 0.5)
	}
	// A sheen along the top, as on the app icons of macOS.
	p.FillGradient(r, ui.LinearGradient{From: ui.RGBA(255, 255, 255, 0.18), To: ui.RGBA(255, 255, 255, 0), Angle: 180, End: 0.6}, rad)
	if !glyph {
		return
	}
	g := prog.TileGlyph
	if g == "" {
		g = prog.Glyph
	}
	s := float32(11)
	p.Icon(icon(g), ui.Rect{X: r.X + (r.W-s)/2, Y: r.Y + (r.H-s)/2, W: s, H: s}, prog.TileFg)
}

// fadeText draws spans on a line, fading the end out when they do not
// fit, rather than cutting them with an ellipsis.
func fadeText(p *ui.Painter, r ui.Rect, spans []ui.Span) {
	key := fmt.Sprintf("%.0f|%v", r.W, spans)
	f, ok := fades[key]
	if !ok {
		f = layoutFade(p, r.W, spans)
		if len(fades) > 512 {
			clear(fades)
		}
		fades[key] = f
	}
	p.RichText(r.X, r.Y+(r.H-f.h)/2, 0, f.spans...)
}

// fades caches the spans of faded texts, by their width and spans.
var fades = map[string]fadeLayout{}

type fadeLayout struct {
	spans []ui.Span
	h     float32
}

func layoutFade(p *ui.Painter, width float32, spans []ui.Span) fadeLayout {
	w, h := p.MeasureText(0, spans...)
	if w <= width {
		return fadeLayout{spans, h}
	}
	r := ui.Rect{W: width}
	const fade = 30
	// Characters are measured one by one; those past the start of the
	// fade lose their opacity as they near the edge.
	var out []ui.Span
	x := float32(0)
	for _, s := range spans {
		start := 0
		for i := 0; i < len(s.Text); {
			_, n := utf8.DecodeRuneInString(s.Text[i:])
			ch := s
			ch.Text = s.Text[i : i+n]
			cw, _ := p.MeasureText(0, ch)
			if x+cw > r.W-fade {
				if start < i {
					head := s
					head.Text = s.Text[start:i]
					out = append(out, head)
				}
				f := 1 - (x+cw/2-(r.W-fade))/fade
				// A color of no alpha is the text's own color: stop
				// before.
				ch.Color = ch.Color.Alpha(max(f, 0) * max(f, 0))
				if ch.Color.A < 4 {
					return fadeLayout{out, h}
				}
				out = append(out, ch)
				start = i + n
			}
			x += cw
			i += n
		}
		if start < len(s.Text) {
			tail := s
			tail.Text = s.Text[start:]
			out = append(out, tail)
		}
	}
	return fadeLayout{out, h}
}
