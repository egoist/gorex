package main

import (
	"embed"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/icons/*.svg assets/brands/*.svg
var assets embed.FS

var svgs = map[string]*ui.SVG{}

// icon returns an embedded SVG: "x" from the icons, "brand:git" from the
// brands.
func icon(name string) *ui.SVG {
	if s, ok := svgs[name]; ok {
		return s
	}
	file := "assets/icons/" + name + ".svg"
	if b, ok := strings.CutPrefix(name, "brand:"); ok {
		file = "assets/brands/" + b + ".svg"
	}
	data, err := assets.ReadFile(file)
	if err != nil {
		panic(err)
	}
	s := ui.MustParseSVG(data)
	svgs[name] = s
	return s
}

// program is how a program shows: its name, its glyph in headers, and the
// tile of tabs.
type program struct {
	Name string
	// Glyph is the icon of the pane's header, in the color of the text.
	Glyph string
	// TileBg and TileFg color the tile of the program in tabs; TileGlyph
	// is the glyph on it, Glyph when empty.
	TileBg, TileFg ui.Color
	TileGlyph      string
	// TileBorder outlines light tiles.
	TileBorder bool
	// Shell is a shell at its prompt.
	Shell bool
}

var (
	shellTile = program{Glyph: "square-terminal", TileGlyph: "terminal", TileBg: ui.Hex("#1d2420"), TileFg: ui.Hex("#5fd38d"), Shell: true}

	programs = map[string]program{
		"codex":     {Name: "Codex", Glyph: "brand:openai", TileBg: ui.Hex("#f7f7f7"), TileFg: ui.Hex("#1a1a1a"), TileBorder: true},
		"claude":    {Name: "Claude Code", Glyph: "brand:claude", TileBg: ui.Hex("#d97757"), TileFg: ui.Hex("#ffffff")},
		"node":      {Name: "Node", Glyph: "node-hex", TileBg: ui.Hex("#5fa04e"), TileFg: ui.Hex("#ffffff"), TileGlyph: "brand:nodedotjs"},
		"bun":       {Name: "Bun", Glyph: "brand:bun", TileBg: ui.Hex("#fbf0df"), TileFg: ui.Hex("#3b2a20"), TileBorder: true},
		"deno":      {Name: "Deno", Glyph: "brand:deno", TileBg: ui.Hex("#1b1b1b"), TileFg: ui.Hex("#ffffff")},
		"python":    {Name: "Python", Glyph: "brand:python", TileBg: ui.Hex("#3776ab"), TileFg: ui.Hex("#ffd43b")},
		"lazygit":   {Name: "Git Changes", Glyph: "plus-minus-circle", TileBg: ui.Hex("#3f8f4f"), TileFg: ui.Hex("#ffffff"), TileGlyph: "diff"},
		"tig":       {Name: "Git Log", Glyph: "git-branch", TileBg: ui.Hex("#f05032"), TileFg: ui.Hex("#ffffff")},
		"git":       {Name: "Git", Glyph: "brand:git", TileBg: ui.Hex("#f05032"), TileFg: ui.Hex("#ffffff")},
		"vim":       {Name: "Vim", Glyph: "brand:vim", TileBg: ui.Hex("#019733"), TileFg: ui.Hex("#ffffff")},
		"nvim":      {Name: "Neovim", Glyph: "brand:neovim", TileBg: ui.Hex("#2b7a3d"), TileFg: ui.Hex("#ffffff")},
		"hx":        {Name: "Helix", Glyph: "brand:helix", TileBg: ui.Hex("#281733"), TileFg: ui.Hex("#c7a3f5")},
		"emacs":     {Name: "Emacs", Glyph: "brand:gnuemacs", TileBg: ui.Hex("#7f5ab6"), TileFg: ui.Hex("#ffffff")},
		"htop":      {Name: "Activity", Glyph: "activity", TileBg: ui.Hex("#1f6f43"), TileFg: ui.Hex("#a7f3c4")},
		"btop":      {Name: "Activity", Glyph: "activity", TileBg: ui.Hex("#1f6f43"), TileFg: ui.Hex("#a7f3c4")},
		"top":       {Name: "Activity", Glyph: "activity", TileBg: ui.Hex("#1f6f43"), TileFg: ui.Hex("#a7f3c4")},
		"ssh":       {Name: "SSH", Glyph: "globe", TileBg: ui.Hex("#5b4bd6"), TileFg: ui.Hex("#ffffff")},
		"mosh":      {Name: "Mosh", Glyph: "globe", TileBg: ui.Hex("#5b4bd6"), TileFg: ui.Hex("#ffffff")},
		"docker":    {Name: "Docker", Glyph: "brand:docker", TileBg: ui.Hex("#2496ed"), TileFg: ui.Hex("#ffffff")},
		"go":        {Name: "Go", Glyph: "brand:go", TileBg: ui.Hex("#00add8"), TileFg: ui.Hex("#ffffff")},
		"cargo":     {Name: "Cargo", Glyph: "brand:rust", TileBg: ui.Hex("#2b2b2b"), TileFg: ui.Hex("#f4a261")},
		"rustc":     {Name: "Rust", Glyph: "brand:rust", TileBg: ui.Hex("#2b2b2b"), TileFg: ui.Hex("#f4a261")},
		"npm":       {Name: "npm", Glyph: "brand:npm", TileBg: ui.Hex("#cb3837"), TileFg: ui.Hex("#ffffff")},
		"pnpm":      {Name: "pnpm", Glyph: "brand:pnpm", TileBg: ui.Hex("#f69220"), TileFg: ui.Hex("#ffffff")},
		"yarn":      {Name: "Yarn", Glyph: "brand:yarn", TileBg: ui.Hex("#2c8ebb"), TileFg: ui.Hex("#ffffff")},
		"ruby":      {Name: "Ruby", Glyph: "brand:ruby", TileBg: ui.Hex("#cc342d"), TileFg: ui.Hex("#ffffff")},
		"irb":       {Name: "Ruby", Glyph: "brand:ruby", TileBg: ui.Hex("#cc342d"), TileFg: ui.Hex("#ffffff")},
		"lua":       {Name: "Lua", Glyph: "brand:lua", TileBg: ui.Hex("#2c2d72"), TileFg: ui.Hex("#ffffff")},
		"php":       {Name: "PHP", Glyph: "brand:php", TileBg: ui.Hex("#777bb4"), TileFg: ui.Hex("#ffffff")},
		"psql":      {Name: "PostgreSQL", Glyph: "brand:postgresql", TileBg: ui.Hex("#336791"), TileFg: ui.Hex("#ffffff")},
		"mysql":     {Name: "MySQL", Glyph: "brand:mysql", TileBg: ui.Hex("#00758f"), TileFg: ui.Hex("#ffffff")},
		"redis-cli": {Name: "Redis", Glyph: "brand:redis", TileBg: ui.Hex("#d82c20"), TileFg: ui.Hex("#ffffff")},
		"tmux":      {Name: "tmux", Glyph: "brand:tmux", TileBg: ui.Hex("#1bb91f"), TileFg: ui.Hex("#ffffff")},
		"make":      {Name: "Make", Glyph: "cpu", TileBg: ui.Hex("#6b7280"), TileFg: ui.Hex("#ffffff")},
		"swift":     {Name: "Swift", Glyph: "brand:swift", TileBg: ui.Hex("#f05138"), TileFg: ui.Hex("#ffffff")},
		"kotlin":    {Name: "Kotlin", Glyph: "brand:kotlin", TileBg: ui.Hex("#7f52ff"), TileFg: ui.Hex("#ffffff")},
	}

	shells = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "dash": true, "nu": true, "pwsh": true, "elvish": true, "xonsh": true, "tcsh": true, "csh": true, "ksh": true,
		"cmd": true, "powershell": true}
)

func init() {
	programs["python3"] = programs["python"]
	programs["ipython"] = programs["python"]
	programs["view"] = programs["vim"]
	programs["vi"] = programs["vim"]
	programs["less"] = program{Name: "Pager", Glyph: "file-code", TileBg: ui.Hex("#475569"), TileFg: ui.Hex("#ffffff")}
	programs["man"] = programs["less"]
}

// programOf returns how a program shows, by the name of its process.
func programOf(name string) program {
	name = strings.TrimPrefix(path.Base(name), "-")
	if p, ok := programs[strings.ToLower(name)]; ok {
		return p
	}
	if shells[name] {
		p := shellTile
		p.Name = name
		return p
	}
	// Any other program: its own name, on a tile of a color of its own.
	p := program{Name: name, Glyph: "square-terminal", TileGlyph: "terminal", TileFg: ui.Hex("#ffffff")}
	p.TileBg = hashColor(name)
	return p
}

// hashColor picks a color for a name, the same every time.
func hashColor(s string) ui.Color {
	palette := []string{"#6d5bd0", "#2f80ed", "#0f9d8a", "#d9822b", "#c2417d", "#3a7d44", "#8a5a44", "#4e5d94"}
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return ui.Hex(palette[h%uint32(len(palette))])
}
