# Rex parity

## Problem Statement

GoRex is meant to be a faithful Go version of Rex, Superlogical's terminal
multiplexer. Today it copies Rex's look and its single-session basics: tabs,
splits, pane headers, sessions that outlive the app, activity dots,
notifications, a command palette. It has roughly 20 of the ~120 actions Rex for
Mac documents, and none of what makes Rex a platform: a server that owns the
model, the `rex` CLI, Lua configuration and scripting, the program status
protocol for agents, multiple sessions and windows, floating panes, the
sidebar, find, theming, and remote servers.

Several of the shortcuts GoRex does have conflict with Rex's: `⌘K` clears the
screen instead of changing session, and `⇧⌘W` closes a tab instead of the
window. Someone who knows Rex is wrong-footed, and a Rex `init.lua`, script or
agent skill can't run against GoRex.

The cause is architectural. In GoRex the app owns the layout and the server
stores it as an opaque blob next to a flat list of PTY sessions. In Rex the
server owns sessions → windows → blocks, and the Mac app, the CLI, Lua scripts,
agents and remote clients are all clients of one documented API. Until GoRex
has the same structure, every missing feature has to be built separately.

## Solution

GoRex adopts Rex's model, vocabulary and public surfaces, so that anything
documented for Rex does the same thing in GoRex:

- The server owns sessions, windows (tabs), blocks (panes), layers (floating
  panes) and clients, exposes Rex's Server API methods with the same names,
  arguments, results and error codes, and emits Rex's events.
- A `rex` CLI with Rex's commands, flags, target rules, `--json` output and
  exit codes.
- Lua 5.1 configuration (`init.lua`) and scripts (`rex do`) with Rex's `rex.*`
  API.
- The app's actions use Rex's action names and default shortcuts, and its
  settings use Rex's settings.
- Terminals implement the Program Status Protocol (OSC 7501), and the agent
  skill and hooks work the same way.
- Servers can be reached remotely the way Rex documents.

**What "faithful" means here.** The Rex documentation at
https://www.superlogical.com/rex/docs is the specification. Its reference
pages (Server API Methods, CLI Commands, Mac Actions, Mac Settings, Lua API,
Events, Program Status Protocol) are the checklist. Where this spec and those
pages disagree, the pages win, except where this spec deliberately leaves
something out of scope. This spec targets the documentation as of 11 November
2026 (Rex Beta 0.1.0, Program Status Protocol revision 0.4). Rex is in beta
and changes quickly, so updating to a later docs revision is its own
deliberate piece of work.

The work is done in phases. Each phase depends on the one before it.

| Phase | Delivers |
| --- | --- |
| 0 | A parity checklist derived from the reference pages, and a parity test that enforces the action table |
| 1 | The server owns the model: sessions, windows, blocks, layers, clients; Rex's methods and events; the app becomes a client |
| 2 | The `rex` CLI |
| 3 | Rex's action names and default shortcuts; `init.lua` with bindings, sequences, modes and custom actions; `rex do` scripts and Lua events |
| 4 | The rest of the Mac client: multiple sessions and windows, sidebar and Activity, floating panes, remaining pane and tab actions, find, prompts, Go to Anything, Quick Look |
| 5 | Agents: OSC 7501, `rex skill`, the Coding Agents and Program Activity settings |
| 6 | Remote: listeners, hosts, Tailscale, `rex server install` over SSH, Linux and FreeBSD servers |
| 7 | Look and settings: themes, imports, typography, cursor, padding, toolbar layouts, onboarding |

## User Stories

### Model and vocabulary (phase 1)

1. As a Rex user, I want GoRex to use Rex's terms (session, window, block, client, layer), so that Rex's docs describe GoRex.
2. As a user, I want a session to be a group of related work that keeps running whether or not any app is looking at it, so that I can close the app and come back.
3. As a user, I want a window to be a tab within a session holding a layout of blocks, so that tabs behave as in Rex.
4. As a user, I want every session, window, block and client to have an ID that never changes and is never reused (`session:<uuid>`, `window:<uuid>`, `block:<uuid>`, `client:<uuid>`), so that scripts can hold on to them.
5. As a user, I want to give sessions, windows, blocks and clients labels I can change, so that I can refer to them by name.
6. As a user, I want the layout to live on the server, so that two apps attached to the same session see the same windows and splits.
7. As a user, I want the server to tell every attached client when the layout changes (`session_view_changed` with a `revision`), so that all clients stay in step.
8. As a user upgrading GoRex, I want my current tabs, splits and running programs carried into the new model, so that the upgrade doesn't lose my work.

### Server API (phase 1)

9. As a script author, I want the server to implement Rex's `session.*` methods (`create`, `list`, `view`, `new_window`, `new_split`, `new_block`, `close_window`, `focus_block`, `focus_direction`, `focus_next_pane`, `focus_previous_pane`, `focus_window`, `focus_next_window`, `focus_previous_window`, `move_block`, `move_window`, `move`, `swap_blocks`, `zoom_block`, `resize_pane`, `set_label`, `set_window_label`, `set_block_label`, `destroy`, `attach`, `attach_all`, `bootstrap`, `detach`, `detach_block`, `list_blocks`, `list_clients`, `describe_block`, `help`), so that calls written for Rex work against GoRex.
10. As a script author, I want `block.close`, `block.event` and `block.method` with Rex's rules (exactly one of `block_id` or `window_id`, `keep_last`, events limited to `key`, `paste`, `mouse`, `wheel`, `focus`), so that block control behaves the same.
11. As a script author, I want terminal blocks to offer Rex's block methods (`com.superlogical.terminal.format`, `.pwd`, `.title`, and the rest of the terminal creator's methods), so that I can read a terminal's screen.
12. As a script author, I want `client.*` methods (`list`, `inspect`, `action`, `perform`, `key`, `keymap`, `reply`, `set_label`, `set_script`), so that I can drive a connected app.
13. As a script author, I want `server.version`, `server.status`, `server.stop`, `server.whoami` and `server.config_reload`, so that I can manage the server.
14. As a user, I want `rex server stop` to stop the server and end every program in it, `--if-idle` to stop it only when it has no sessions and no other client (exit 1 and say what's using it otherwise), and `--discard` to delete the saved sessions too, with `server.stop` accepted only over the local socket or named pipe, so that I can stop the server safely and nobody can stop it remotely.
15. As a script author, I want `target.resolve` and `target.complete`, so that target matching is the same everywhere.
16. As a script author, I want `help.all` and per-method schemas (input schema, output schema, request and response examples, validation notes), so that the API documents itself as Rex's does.
17. As a script author, I want errors to carry Rex's codes (`bad_request`, `not_found`, `session_not_found`, `conflict`, `method_not_found`) and a status, so that my error handling ports unchanged.
18. As a script author, I want misspelled arguments rejected (`additionalProperties: false`), so that typos fail loudly.
19. As a client author, I want `data.connect` tickets and a data connection that streams a block's content, so that terminal output streams the way Rex's does.

### Events (phase 1)

20. As a script author, I want session events (`window_created`, `window_closed`, `window_label_changed`, `active_window_changed`, `block_created`, `block_closed`, `session_label_changed`, `session_view_changed`, `client_connected`, `client_disconnected`, `client_connection_changed`, `creator_changed`), so that I can react to layout changes.
21. As a script author, I want terminal events (`child_exited`, `pwd_changed`, `title_changed`, `bell`, `process_changed`, `desktop_notification`, `progress_report`, `clipboard_written`, `program_status_changed`, `program_status_removed`) with Rex's fields, so that I can react to what programs do.
22. As a script author, I want server events (`session_created`, `session_destroyed`, `client_joined`, `client_left`, `client_changed`, `keymap_changed`, `hosts_changed`, `tailscale_status_changed`), so that I can watch the whole server.
23. As a script author, I want events wrapped exactly as Rex wraps them (`session_event` → `block_event` → payload) in JSON, so that consumers of `rex events --json` work unchanged.

### CLI (phase 2)

24. As a user, I want `rex` on the `PATH` of every terminal GoRex opens, with `REX_BIN_DIR` set, so that I can use the CLI anywhere inside GoRex.
25. As a user, I want `REX_SESSION`, `REX_BLOCK` and `REX_SERVER` set in every terminal, so that commands default to the terminal I type them in.
26. As a user, I want the common commands (`rex new`, `ls`, `run`, `split`, `do`, `send`, `send-key`, `capture`, `kill`), so that everyday automation works.
27. As a user, I want the layout commands (`rex focus`, `zoom`, `move`, `swap`, `resize`), so that I can rearrange panes from a script.
28. As a user, I want the resource commands (`rex session …`, `rex window …`, `rex block …`, `rex client …`) with Rex's subcommands and flags, so that every object can be managed from the CLI.
29. As a user, I want `rex events`, `rex wait`, `rex api list|describe|call` and `rex actions`, so that I can automate and explore the API.
30. As a user, I want `rex server run|start|status|logs|stop`, `rex config check|reload`, `rex keymap`, `rex whoami` and `rex version`, so that I can administer the server.
31. As a user, I want `rex completion` for bash, zsh, fish, PowerShell and nu, so that the CLI completes targets and commands.
32. As a user, I want `rex terminfo status|setup|remove|show` and `rex terminfo host …`, so that remote shells know the terminal's capabilities.
33. As a user, I want targets matched as Rex does (full ID, then case-insensitive label, then position from one, then an ID suffix of at least four characters), so that `-s`, `-w`, `-b` and `-C` mean the same thing.
34. As a user, I want an ambiguous target to stop the command, list the matches and exit with status 3, so that a script never acts on the wrong thing.
35. As a user, I want `--json` on listing commands with full IDs, so that output is stable to parse.
36. As a user, I want Rex's exit statuses (0, 1, 2, 3, 4, 130, and the program's own status for `rex wait` and `rex run --wait`), so that shell scripts can branch on them.
37. As a user, I want the global options (`--autostart`, `-C`, `--color`, `-S`, `--timeout`, `-v`), so that every command takes them.
38. As a user, I want `rex api call` results indented on a terminal and on one line when piped, so that they feed `jq`.

### Actions, keys and Lua (phase 3)

39. As a Rex user, I want every app action named as Rex names it (`client.tab.new`, `pane.split.right`, `session.switch`, …), so that `rex.bind` and `rex do` take the same names.
40. As a Rex user, I want every action's default shortcut to be Rex's, so that my muscle memory works.
41. As a Rex user, I want `⌘K` to open Change Session, `⇧⌘W` to close the window, `⌥⌘W` to close the tab, `⌘]`/`⌘[` to focus the next/previous pane, `⌃Tab`/`⌃⇧Tab` to change tab, `⇧⌘=` to increase the font size and `⌘R` to refresh sessions, so that GoRex no longer contradicts Rex.
42. As a user, I want the command palette to list every action under Rex's groups (Window, App, Session, Host, Tab, Pane, Keys, Find, View), so that I can find any action.
43. As a user, I want `init.lua` in Rex's location, loaded at start and reloaded with `client.config.reload` or `rex config reload`, so that I configure GoRex as I configure Rex.
44. As a user, I want `rex.bind` and `rex.unbind` for actions, actions with arguments and Lua functions, so that I can change shortcuts.
45. As a user, I want key sequences (two or more keys in a row), so that I can use leader-style bindings.
46. As a user, I want key modes (`rex.mode`, `client.mode.enter`, `client.mode.exit`, `client.mode.exit_all`), so that ordinary keys can do something else for a while, such as resizing panes with arrows.
47. As a user, I want custom actions (`rex.action{…}`) that work like built-in ones, so that they can be bound, run from the palette and called with `rex do`.
48. As a user, I want `rex do` to run an action, a script file or `-e` code, with `KEY=VALUE` arguments in `rex.args`, so that I can script GoRex.
49. As a script author, I want `rex.call`, `rex.session.<method>`, `rex.block.call`, `rex.layout`, `rex.invoke` and `rex.client.queue`, so that scripts can do anything the API can.
50. As a script author, I want `rex.on`, `rex.wait`, `rex.sleep`, `rex.stop`, `rex.after` and `rex.every`, so that scripts can react to events and keep running.
51. As a script author, I want `rex.exec`, `rex.exec.start`, `rex.json.encode|decode` and `rex.log`, so that scripts can run programs and handle JSON.
52. As a script author, I want each Lua function to work, warn, fail or be missing in exactly the contexts Rex's "Where Each Function Works" table says, so that shared config files behave the same.
53. As a user, I want `rex config check` to report errors in `init.lua` without applying it, so that a bad config doesn't break my keys.
54. As a user, I want Change Shortcut… (`client.shortcuts.edit`), so that I can rebind a key from the app.

### Mac client (phase 4)

55. As a user, I want several sessions on one server, with New Session (`⇧⌘N`), Change Session (`⌘K`), Go to Session, Next/Previous Session, Rename Session and Close Session, so that I can keep projects apart.
56. As a user, I want several app windows (`⌘N`, `⇧⌘W`, `⌃⌘F`), so that I can put sessions on different screens.
57. As a user, I want `⌘W` (`client.close`) to close the focused pane, else the selected tab, else the window, so that it behaves as in Rex.
58. As a user, I want a sidebar (`⌘/`) listing sessions and their tabs, with collapse and expand, a tab filter and Activity (`⌘.`) by session or by priority, so that I can see all my work at once.
59. As a user, I want tabs I can close (others too), rename, go to by number (`⌘1…9`), move backward and forward (`⌃⇧PageUp`/`PageDown`) and move to a new window, so that tab management matches Rex.
60. As a user, I want to split left, right, up, down and best fit (`⇧⌘←`, `⌘D`, `⇧⌘↑`, `⇧⌘D`, `⌘↩`), so that I can split any way.
61. As a user, I want to move panes (`⌥⇧⌘`+arrows), swap them, move a pane to a new tab or another tab, close other panes, balance splits and resize, so that I can rearrange a layout.
62. As a user, I want floating panes: float a pane (`⌥⌘↩`), a new floating pane, minimise one (`⌥⌘M`) or all (`⌥⇧⌘↩`), raise, lower and resize them, so that I can keep a terminal on top.
63. As a user, I want Zoom Pane (`⇧⌘↩`), Rename Pane, Copy Pane ID, Paste into Pane, Copy from Pane, Select All in Pane and Send Key (`⌘⌫`), so that pane actions match Rex.
64. As a user, I want Find (`⌘F`, `⌘G`, `⇧⌘G`) in a pane's scrollback, so that I can search output.
65. As a user, I want Jump to Previous/Next Prompt (`⌘↑`/`⌘↓`) using OSC 133, so that I can move between commands.
66. As a user, I want Go to Anything (`⌘P`), Go to Directory (`⇧⌘G`), Insert Path (`⌃⌘I`) and View Activity, so that I can navigate quickly.
67. As a user, I want Quick Look (`⌘Y`), so that I can preview a file named on screen.
68. As a user, I want the notice banner and Dismiss Notice (`⇧⌘.`), so that in-app notifications behave as in Rex.
69. As a user, I want Rex Diagnostics (`⇧⌘,`), Copy Version Info, Copy Debug Info and Restart Server…, so that I can troubleshoot.
70. As a user, I want Toggle Visibility as a global hotkey, so that I can summon GoRex from any app.

### Agents (phase 5)

71. As an agent author, I want GoRex terminals to implement OSC 7501 revision 0.4 exactly (syntax, records and ids, states, keys, feature detection, limits, lifetime rules), so that programs that report status to Rex report it to GoRex.
72. As a user, I want tabs, pane headers, the sidebar and Activity to show each program's state (idle, working with progress, done, blocked with its kind and message, error), so that I can see what my agents are doing.
73. As a user, I want `working` and `blocked` records dropped when the program exits or a new prompt begins, and `done` and `error` kept until I look, so that status is never stale.
74. As a user, I want `rex skill status|setup|remove|show` and Install Rex for Agents (`client.agents.setup`), so that Claude Code, Codex and other agents get Rex's skill and hooks.
75. As an agent, I want to open tabs, split panes, read a terminal's screen and control connected apps through the CLI, so that I can work as I would in Rex.
76. As a user, I want the Coding Agents, Agent Behavior, In-App Notifications, Program Activity and System Notifications settings, so that I control how agent activity reaches me.
77. As a user, I want OSC 9 and OSC 777 desktop notifications and the bell to raise notifications and the Dock badge, so that programs can get my attention.

### Remote (phase 6)

78. As a user, I want a GoRex server to run on Linux and FreeBSD, so that I can use a remote machine as a host.
79. As a user, I want the server to listen only on Unix sockets by default, and to add network listeners (`--listen`, `--extra-listen` with `unix://`, `tailnet://`, `tailnet-serve://`, plain HTTP only with `--allow-anonymous`), so that remote access is explicit and safe.
80. As a user, I want to reach a remote server over a forwarded SSH socket, so that I can use it without opening a port.
81. As a user, I want hosts (`rex hosts list|add|edit|remove|check`, `rex.host{…}`, `rex home`) and Switch Host, Add Remote Host and Remove Host in the app, so that I can move between servers.
82. As a user, I want `rex -S <host>` to act on another server, so that the CLI works across machines.
83. As a user, I want Tailscale built into the server (`rex server tailscale on|off|login|logout|rename|pair`), so that devices on my tailnet can connect.
84. As a user, I want `rex server install ssh://…` and `rex server uninstall`, so that I can set up a remote server from my Mac.
85. As a user, I want a client whose connection drops to show it and reconnect (`client_connection_changed`), so that a flaky network doesn't lose my place.

### Look and settings (phase 7)

86. As a user, I want Rex's settings, grouped as Rex groups them (Integrations, Notifications, Server, Interface, Themes, Terminal, Remote Hosts, Developer), so that every setting is where I expect.
87. As a user, I want light and dark themes chosen separately, Change Theme…, Switch Appearance Mode and Import Theme File (Ghostty and iTerm themes), so that I can match my other tools.
88. As a user, I want typography, advanced font, colours, cursor, keyboard, scrolling, resizing, selection (including copy on select), links, path browser, titles and shell settings, so that the terminal behaves as I like.
89. As a user, I want Interface settings (appearance, toolbar, tab behaviour, tab icons, panes, window, padding, active pane, splits and focus including focus follows mouse, gestures, animation, launch), so that the app looks and behaves as I like.
90. As a user, I want both toolbar layouts (`⇧⌘S`), Reveal Toolbar (`⌥⌘T`) and Reveal Pane Header (`⌥⇧⌘T`), so that I can choose a compact or full layout.
91. As a new user, I want Open Onboarding Session…, which imports keybindings and themes from tmux, Zellij and Ghostty and sets up agents, so that I can start quickly.
92. As a user, I want Copy Config File Path, so that I can find `init.lua`.

## Implementation Decisions

- **Rex's docs are the specification.** Names, arguments, results, error
  codes, event shapes, CLI commands and flags, exit codes, action names,
  default shortcuts, settings and Lua functions follow the reference pages
  exactly. GoRex does not invent its own names for anything Rex documents.
- **Compatible at the API surface.** GoRex implements Rex's documented API
  surface: method names, JSON arguments and results, schemas, error codes,
  event JSON, the HTTP paths the docs name (`/api/app`, `/api/data`,
  `/api/help`), and the data connection's ticket flow. A Rex script, `init.lua`
  or agent skill should then run against GoRex unchanged. Byte-level
  compatibility with Rex's own binaries (Rex's CLI or Mac app talking to a
  GoRex server, or the reverse) depends on transport details the docs don't
  give; see Open Questions.
- **The server owns the model.** The session server stops storing the app's
  layout as an opaque blob. It holds sessions → windows → layouts of blocks,
  plus floating layers, focus, zoom and labels, and every change goes through
  an API method and emits events. The app keeps no layout state of its own: it
  renders `session.view` and applies `session_view_changed`.
- **Blocks have creators.** A block is created by a block creator; the
  terminal's creator is `com.superlogical.terminal`, and its methods and events
  live under that name, as in Rex. Today's PTY session becomes the terminal
  block's implementation.
- **The CLI is a client.** `rex` is a thin client of the Server API. It adds
  target resolution (through `target.resolve`), table and `--json` output and
  exit codes, and no logic the API lacks.
- **Lua is Lua 5.1** with the full standard library, matching Rex. One Lua
  runtime serves both the configuration file and `rex do` scripts, with the
  per-context rules from Rex's "Where Each Function Works" table.
- **Actions are a registry.** The app's commands become a registry keyed by
  Rex's action names, with Rex's titles, groups and default shortcuts. Menus,
  the palette, the keymap, `client.action`, `client.perform`, `rex do` and
  `rex actions` all read from it.
- **Shortcuts on other platforms.** On macOS the defaults are exactly Rex's.
  Elsewhere GoRex keeps its current translation, where `⌘` becomes `Ctrl+Shift`
  so Control and a letter still reach the terminal. Rex documents no Windows
  or Linux client to follow.
- **Current shortcuts that change to match Rex:**

  | GoRex today | Rex |
  | --- | --- |
  | `⌘K` clears the screen | `⌘K` is Change Session (`session.switch`) |
  | `⇧⌘W` closes the tab | `⇧⌘W` closes the window; Close Tab is `⌥⌘W` |
  | `⇧⌘]` / `⇧⌘[` change tab | `⌃Tab` / `⌃⇧Tab` change tab; `⌘]` / `⌘[` focus the next/previous pane |
  | `⌘=` increases the font size | `⇧⌘=` |
  | `⇧⌘R` renames the tab | Rename Tab has no default; `⌘R` is Refresh Sessions |
  | `⌃⌘`+arrows move a divider | Resize Pane has no default (Rex resizes through a key mode); `⌥⇧⌘`+arrows move the pane |
  | `⌃⌘=` equalises panes | Balance Splits has no default |
  | `⌘P` opens the palette at panes | `⌘P` is Go to Anything |

- **OSC 7501 follows revision 0.4** of the Program Status Protocol. Terminal
  parsing feeds `program_status_changed` and `program_status_removed`, and the
  UI reads records from the server like any other client.
- **Platforms.** The server runs on macOS, Linux, FreeBSD and Windows. The app
  runs on macOS and, as today, on Windows.

## Testing Decisions

- Tests check behaviour seen from outside: API results and events, CLI output
  and exit codes, and what the app shows and does. Not internal structures.
- **Seam 1: the server's API through a client.** It covers phases 1, 2, 5 and
  6. Each test starts a server with its own state directory and drives it
  through the client package, as the existing server test does. CLI tests run
  `rex` commands against that server and assert on output and exit status.
  OSC 7501 tests write sequences into a terminal block and assert on
  `program_status_changed`/`program_status_removed` and the records the server
  reports.
- **Seam 2: the app's view through the UI tester.** It covers phases 3, 4 and
  7. Tests send keys and run actions in a headless app on its own server, as
  the existing app tests do, and assert on what is shown and on the server's
  view. Lua configuration is tested here: load an `init.lua`, press a key,
  assert on the result.
- **Parity test.** A table of Rex's Mac actions (name, group and default
  macOS shortcut), transcribed from the reference page, is checked against
  the action registry. A missing action, or one whose name or default
  shortcut differs, fails the test, except for actions this spec places out of
  scope, which are listed in the table as excluded. The same applies to
  `rex api list` against the Server API Methods page, and to the CLI's
  commands against the CLI Commands page.
- Prior art: the existing server test and the existing app tests (panes and
  tabs, restore, labels, close buttons, palette scrolling) set up both seams.

## Open Questions

- **Byte-level compatibility with Rex.** The docs give method semantics but
  not the control connection's framing, its authentication handshake, or the
  `rex.data.terminal.v1` data protocol (HELLO and REJECT messages are named,
  their encoding isn't). Getting there would mean observing a real Rex
  install. Should GoRex aim for it (Rex's CLI and app work with GoRex servers,
  and the reverse), or stop at the documented surface?
- **Copying the docs into the repo.** A snapshot of the reference pages in the
  repo would let the parity tests be regenerated, but the docs are
  Superlogical's, so it needs their licence or permission. Without it, the
  parity tables are transcribed and cite the source pages.
- **GoRex's own extras.** Quit and End All Sessions (`⌥⌘Q`) and Clear Screen
  and Scrollback have no Rex action. Keep them under new names, or drop them?
- **Upgrade path.** Migrate the current saved layout and running sessions into
  the new model, or accept a one-time reset? A migration needs the old format
  to be readable by the new server.
- **State and binary names.** Rex's state locations, socket path and
  `REX_*` variables vs GoRex's current `GOREX_DIR`. Adopting Rex's exactly
  means GoRex and a real Rex can't be installed side by side.
- **Docs drift.** Rex changes quickly. How often should GoRex move to a newer
  docs revision, and who decides?

## Out of Scope

- Rex for iOS and its actions and settings.
- The web client and the Web Client Origins setting.
- Automatic updates and update channels (`client.update.*`).
- The shared server and login server modes on Linux (the `rex` system
  account, PAM), and packaging with system package managers and Nix modules.
  They follow once remote access works.
- macOS privacy helpers (`server.apple.*`, Grant System Permissions…), unless
  GoRex hits the same permission prompts.
- The Rex community MCP server and Discord integration.
- Pair Device with QR Code, which only exists to pair Rex for iOS.

## Further Notes

- **The code disagrees with the spec.** The server stores the app's layout as
  an opaque blob and knows only flat PTY sessions. The app owns tabs, splits,
  focus and zoom. That is the reverse of Rex's design and the reason phase 1
  comes first.
- **Vocabulary changes.** GoRex's "session" (one PTY) becomes a terminal
  block. Rex's "session" is a group of windows. GoRex's "tab" is Rex's
  "window", and the app's OS window is a client window.
- Sources, all fetched 11 November 2026: the Rex docs index
  (https://www.superlogical.com/rex/docs.md) and its reference pages for
  Server API Methods, CLI Commands, Mac Actions, Mac Settings, Lua API,
  Events, and the Program Status Protocol (revision 0.4, 2026-10-10). Every
  page is available as markdown by adding `.md` to its address.
