# GoRex

A replica of [Superlogical's Rex](https://www.superlogical.com/updates/public-testing-beginning)
terminal, written in Go with [MyGo](https://mygo.egoist.dev)'s native UI and its
terminal plugin (Ghostty's libghostty-vt). No webview, no cgo: one ~15 MB app.

![GoRex](docs/screenshot.png)

## What it does

- **Tabs and split panes.** Every pane is a card over the window's gradient,
  with its program's icon, name and directory in its header, and buttons to
  split right, split down, zoom and close. Drag the gaps between panes to
  resize them (double-click resets a split to half).
- **Persistent sessions.** Shells run in a session server, a background
  process of the same binary (`GoRex -server`). Quit GoRex and everything
  keeps running; open it again and every tab, split and pane comes back as
  it was, programs still running. The server keeps each session's screen in
  a headless libghostty-vt emulator and sends a snapshot on attach, so
  full-screen programs (lazygit, vim, htop…) come back exactly.
- **Program activity.** The server watches each terminal's foreground
  process (`tcgetpgrp`, `sysctl`, `proc_pidinfo`; on Windows, the shell's
  newest child, its command line and directory): headers and tabs name the
  program (`Node`, `Git Changes` for lazygit, `Codex`, `SSH host`…) and its
  working directory, a green dot shows a program printing, and an orange dot
  marks a pane whose program finished or rang the bell out of sight. When
  the window is in the background, a long command finishing posts a
  notification.
- **Tab icons** stack the tiles of a tab's programs, the focused pane's in
  front, as Rex does. Tabs reorder by dragging, rename by double-click, and
  have a context menu.
- **Host chip** with the machine's name and model; its popover shows the
  chip, memory, OS and the session server.
- **Command palette** (⇧⌘P, Ctrl+Shift+P, or the ⌘ button): every command, and every pane
  to jump to, fuzzy-matched.
- Light and dark appearances (View ▸ Appearance), text size (⌘+ ⌘− ⌘0),
  JetBrains Mono embedded.

## Shortcuts

| macOS | Windows, Linux | |
|---|---|---|
| ⌘T | Ctrl+Shift+T | New tab |
| ⌘D / ⇧⌘D | Ctrl+Shift+D / Ctrl+Shift+E | Split right / down |
| ⌘W / ⇧⌘W | Ctrl+Shift+W | Close pane / tab |
| ⇧⌘↩ | Ctrl+Shift+Enter | Zoom the pane |
| ⌥⌘ arrows | Alt+arrows | Focus the pane in that direction |
| ⌃⌘ arrows | Alt+Shift+arrows | Move the nearest divider |
| ⌃⌘= | | Equalize panes |
| ⌘1…⌘9, ⇧⌘[ ⇧⌘], ⌃Tab | Ctrl+Shift+1…9, Ctrl+PageUp/PageDown, Ctrl+Tab | Switch tabs |
| ⇧⌘R | Ctrl+Shift+R | Rename tab |
| ⇧⌘P, ⌘P | Ctrl+Shift+P | Command palette |
| ⌘K | Ctrl+Shift+K | Clear |
| ⌘+ ⌘− ⌘0 | Ctrl+= Ctrl+- Ctrl+0 | Text size |
| ⌥⌘Q | | Quit and end all sessions |

Elsewhere than on macOS, Control and a letter are the terminal's (Ctrl+C,
Ctrl+D, Ctrl+W…), so GoRex's shortcuts take Shift too, as copying and
pasting do (Ctrl+Shift+C, Ctrl+Shift+V), and none take Control and Alt,
which type characters (AltGr) on many keyboards.

### Settings

Settings… (⌘, on macOS, Ctrl+, elsewhere; Shell menu and command palette)
opens a window of its own with a sidebar of sections: Appearance (light,
dark or system; text size), Terminal (font, cursor style and blink, copy on
selection), Theme (any of Ghostty's themes, or follow the config),
Keyboard (GoRex's shortcuts, read only), Remote (see
[Remote access](#remote-access)) and About (version, Check for Updates…).
The choices go to `settings.json`.

Remote turns the server's remote access on and off, and manages it: the
list of allowed clients (add one by name and public key, revoke one), this
machine's own key to copy, and, while listening, the address, hidden until
you show it, with Copy and Regenerate (which asks first). The switch stays
off until a client is allowed. The page asks the server every two seconds
while it shows. It does not connect to other hosts yet.

Precedence: what you set in Settings, over Ghostty's config, over GoRex's
defaults. "Reload Ghostty Config" reads the config again but never
replaces a choice made in Settings; "Use Ghostty config" (Terminal)
forgets the font, cursor, and copy-on-selection choices, and Theme's
"Follow Ghostty config" the theme's. A theme chosen in Settings also
replaces the config's `background`, `foreground`, `palette`, and the other
colors, which belonged to its own theme. Font, theme, copy on selection,
and the cursor apply to open panes at once.

### Ghostty configuration

GoRex reads Ghostty's config: `$XDG_CONFIG_HOME/ghostty/config.ghostty`
(`config`), else `%LOCALAPPDATA%` on Windows or `~/.config`; on macOS also
`~/Library/Application Support/com.mitchellh.ghostty/`. `config-file`
includes work. `GOREX_GHOSTTY_CONFIG` names one file instead.

Supported: `font-family`, `font-size`, `font-feature`, `font-thicken`,
`adjust-cell-height` (percent), `theme` (also `light:…,dark:…`),
`background`, `foreground`, `cursor-color`, `cursor-text`,
`selection-background`, `selection-foreground`, `palette` (0–15),
`cursor-style`, `cursor-style-blink`, `scrollback-limit`,
`macos-option-as-alt`, `copy-on-select` (`clipboard`; `true` is
Linux's selection clipboard, so it copies nothing), `right-click-action`,
`minimum-contrast`, `keybind`. `keybind` takes `clear`, `performable:`,
and the actions unbind, ignore, copy_to_clipboard, paste_from_clipboard,
select_all, clear_screen, increase_font_size, decrease_font_size,
reset_font_size, and scroll_page_up, scroll_page_down, scroll_to_top,
scroll_to_bottom; `global:`, `all:`, and `unconsumed:` binds, sequences
(`a>b`), and other actions are skipped. GoRex's own shortcuts come
before keybinds. A text size set with ⌘+ ⌘− wins over `font-size`;
⌘0 goes back to it. With a theme or background, panes draw its
background, and unless View ▸ Appearance names one, the window turns
dark or light by it, as Ghostty's `window-theme = auto` does (not with
`light:…,dark:…`, which follows the system). "Reload Ghostty Config"
(command palette, View menu) applies fonts, themes, `copy-on-select`,
and `minimum-contrast` to open panes; the other options apply to new
panes.

## Remote access

The session server can also listen on a
[tailcat](https://github.com/tailscale/tailcat) address: Tailscale's data
plane without its control plane, so WireGuard end to end, through a DERP
relay or directly, with no Tailscale account. Remote access is off by
default and turns on only once at least one client is allowed: each
machine has a client key (`remote-client.json`), and the server lets in
the keys of its allowlist only. Removing the last client turns remote
access off. The server keeps its keys (`remote.json`, mode 0600), so its
address stays the same; regenerating them makes a new one, and the old
address stops working.

What to know before turning it on:

- The address is a secret: it holds the server's key and the WireGuard
  pre-shared key. Do not paste it where others read it. GoRex never logs
  it, nor any key.
- The allowlist is what keeps others out. A leaked address alone gets
  nobody in, but regenerate it anyway.
- An allowed key means running commands as this user: a client may
  start any program with any environment and attach to every session.
  From a shell there it can reach the local socket, and so change the
  layout and remote access itself. Trust it as you would an SSH key.
- Over the connection itself, a remote client may list, create, kill,
  resize and attach to sessions only. It may not shut the server down,
  read or set the window layout, or see or change remote access
  (`remote.*`): those are for the local socket only.
- Every connection is limited: a line of requests of at most 1 MiB, a
  terminal of 1 to 1000 columns and rows, and 8 kills at once.

## Develop and build

GoRex needs [Go](https://go.dev/dl/) 1.27+ and MyGo 0.6.2, whose CLI
`go.mod` pins as a tool:

```sh
go tool mygo dev     # GoRex Dev, rebuilt and restarted as the code changes
go test ./...        # the server, and the view without a window
go tool mygo build   # build/darwin-arm64/GoRex.app and a .dmg
go run ./tools/mkicon  # redraw resources/icon.png
```

`go get -tool github.com/egoist/mygo/cmd/mygo@latest` updates MyGo and its
CLI together.

### Release

Releases update installed apps, signed with a key that only the
maintainer holds. Once, run `go tool mygo keygen` and put the contents of
`mygo-update.pub` in `updates.publicKey` of `mygo.json`; keep
`mygo-update.key` out of the repository. For each release, add a
`## 0.1.0` section (the version of `mygo.json`) to `CHANGELOG.md`, which
becomes the release notes, then build and upload to a draft GitHub release
(it needs `gh` or `GH_TOKEN`), and publish the draft:

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat path/to/mygo-update.key)" go tool mygo build -upload
```

Development builds and apps that cannot write where they are installed
do not update; "Check for Updates…" is in the Shell menu and the command
palette.

The app keeps its state in its data directory: the server's socket and
log, `layout.json` and `settings.json`, in `~/Library/Application
Support/GoRex` for the built app and `GoRex Dev` for `mygo dev`'s, so
that developing never touches the sessions of the app you use
(`GOREX_DIR` names another directory). The sessions outlive the app, and
a new build: on macOS and Linux, a build that finds a server an older
build started has it run the new build in its place, in the same
process, its shells still running and their screens kept. On Windows a
development build replaces such a server, ending its sessions, which
then start again in the same directories, and the built app keeps it.

### Windows

GoRex runs on Windows 10 1809 or later, whose ConPTY runs the shells; its
data directory is `%AppData%\GoRex`. Panes run `$SHELL` when it names a
Windows program (`nu`, `C:\Program Files\Git\bin\bash.exe`), else
PowerShell (`pwsh`, then `powershell`), else `cmd`. PowerShell's `cd`
leaves its process's directory where it was, so GoRex starts it with a
prompt that sets it, wrapping the profile's own prompt: the header then
follows it, and new panes open there. The window's title bar is hidden
and the system's buttons with it: the title bar draws minimize, maximize and
close with the other buttons, and the window still resizes from every edge,
its top included.

## Layout

| | |
|---|---|
| `main.go` | the app, its window, and `-server` |
| `state.go` | tabs, the tree of splits, panes, saving and restoring the layout |
| `view.go`, `tabs.go`, `host.go`, `commands.go` | the interface: title bar, tabs, panes, host popover, menus and palette |
| `programs.go` | how programs show: names, glyphs, tile colors |
| `style.go`, `settings.go` | colors, fonts, terminal themes; appearance, text size and the user's choices over Ghostty's |
| `settingsview.go`, `remoteview.go` | the Settings window, and its Remote page |
| `tools/remotectl` | runs and probes the session server without the app: allow, enable, probe over tailcat |
| `CHANGELOG.md` | release notes: a `## <version>` section per release |
| `internal/rex` | the session server and its client: PTYs, foreground processes, headless screens, the socket protocol |

Rex's connections to servers on other machines are only half there: the
server side exists (remote access, above), but the window cannot open
remote hosts yet.

Icons: [Lucide](https://lucide.dev) (ISC) and [Simple Icons](https://simpleicons.org)
(CC0). Font: [JetBrains Mono](https://www.jetbrains.com/lp/mono/) (OFL).
