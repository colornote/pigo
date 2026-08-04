# Browser — CDP Chrome automation package

A **pi-style package** (see `docs/packages.md`) providing Chrome-browser-like
automation for coding agents. Zero third-party Go dependencies — the CDP
client and WebSocket are implemented on the Go standard library.

It follows the topromax-ops browser baseline: attach to a **persistent Chrome
debug profile** on `localhost:9222` (`--remote-allow-origins=*` +
`--user-data-dir="$HOME/.chrome-cdp-profile"`), serialize access with the
shared flock at `/tmp/topromax-cdp.lock`.

This package is **independent of PiGo's core** — it does not modify the agent
loop, tool registry, or CLI. It is a standalone capability an agent can invoke
by reading `SKILL.md` and calling `bin/browser`.

## Layout

```text
packages/browser/
├── package.json           # pi manifest (skills + scripts)
├── SKILL.md               # Agent Skills spec — how an agent should use it
├── README.md
├── Makefile
├── go.mod                 # module pigo/browser (Go 1.18)
├── cmd/browser/main.go    # CLI: open/tabs/eval/click/type/text/scroll/shot/...
└── internal/cdp/
    ├── ws.go              # stdlib RFC 6455 WebSocket client (handshake/mask/fragments/ping-pong)
    └── cdp.go             # CDP client: /json/list, /json/new, Runtime.evaluate, Page.captureScreenshot
```

## Build & install

```bash
cd packages/browser
make            # builds bin/browser
make test       # go vet + go test ./...

# As a pi package (pi reads package.json `pi` manifest):
pi install ./packages/browser
# Or try it without installing:
pi -e ./packages/browser
```

## CLI

```text
browser doctor                        preflight check + launch hint
browser open <url>                    open new tab and navigate
browser tabs [filter]                 list tabs (index, title, url)
browser eval <tab> '<js>'             run JS, print JSON result
browser click <tab> <selector>        click first matching element
browser type <tab> <sel> <text>       set input value + input/change events
browser text <tab> [selector]         page/element innerText
browser scroll <tab> top|bottom|by <px>
browser shot <tab> <file.png>         screenshot to PNG
browser close <tab>                   close tab
browser wait <ms>                     sleep helper
browser lock -- <cmd...>              run cmd under the CDP flock
```

`<tab>` is a 0-based index from `browser tabs`, or a URL/title substring.
Port overridable via `CDP_PORT` (default 9222).

## Why pure stdlib

- No `chromedp`/`websocket` deps → the binary builds anywhere Go does, fits
  the project's "no new third-party dependencies" rule.
- `Runtime.evaluate` is the universal escape hatch: click/type/text/scroll are
  thin wrappers over JS, so any DOM capability is reachable without protocol
  surface growth.

## Integration with topromax-ops

Designed to interoperate with `automation/` (browser_use) and the
chrome-devtools MCP:

- Same Chrome baseline (`:9222`, `~/.chrome-cdp-profile`, `--remote-allow-origins=*`)
- Same serialization lock (`/tmp/topromax-cdp.lock`) — `browser lock -- ...`
  blocks until other agents are done, and other agents' flocks block this CLI
- `browser doctor` surfaces the same launch hint as AGENT.md §11.1.1

## Security

The package has full access to the attached Chrome session (cookies, logged-in
platforms). Only run it against profiles you own; review commands before
executing (same trust model as pi packages — see `docs/packages.md`).
