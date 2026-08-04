---
name: browser
description: Drive a persistent Chrome profile over CDP (localhost:9222) like a browser — open tabs, navigate, click, type into inputs, read page text, scroll, and screenshot. Use whenever a task requires browsing a live website, filling forms, verifying a rendered page, or scraping visible content.
---

# Browser (CDP Chrome automation)

A Chrome-browser-like capability package: a zero-dependency Go CLI that
attaches to a **persistent Chrome debug profile** and lets the agent operate
a real browser — exactly the pattern used by topromax-ops
(`automation/src/automation/core/browser.py`).

## Prerequisites (topromax-ops AGENT.md §11.1.1 baseline)

Chrome must be running with the debug profile:

```bash
lsof -ti :9222 | xargs -r kill -9 2>/dev/null
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
   --remote-debugging-port=9222 \
   --remote-allow-origins=* \
   --user-data-dir="$HOME/.chrome-cdp-profile" \
   --no-first-run 2>/dev/null &
export NO_PROXY=localhost,127.0.0.1   # REQUIRED: CDP websockets must not hit a SOCKS proxy
```

Verify: `browser doctor` → `✓ Chrome CDP :9222 reachable`.

> `--remote-allow-origins=*` is mandatory for external WS clients (else
> Chrome 403s every connection). Missing it → "Rejected an incoming WebSocket
> connection".

## CDP serialization (topromax-ops §11.1.2)

The shared profile must never be written concurrently — cookies/sessionStorage
get corrupted and every platform logs out. Wrap multi-step browser sessions in
the flock:

```bash
browser lock -- browser open https://example.com && ...
```

or, when another agent (browser_use / MCP) holds `/tmp/topromax-cdp.lock`,
`browser lock` blocks until it is free.

## Command reference

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

## Worked example (agent workflow)

```bash
# 1. Preflight
browser doctor

# 2. Open a page (or reuse an existing logged-in tab by URL substring)
browser open https://medium.com/new-story

# 3. Inspect what rendered
browser text 0 'h1, .title'        # verify page loaded
browser eval 0 'document.title + " | " + location.href'

# 4. Fill a form
browser type 0 'textarea[contenteditable]' 'Draft headline...'
browser click 0 'button[data-testid="publish"]'

# 5. Verify + capture
browser text 0
browser shot 0 /tmp/page.png       # then use the `vision` tool on it

# 6. Serialize the whole session against the shared profile
browser lock -- sh -c 'browser open ... && browser type ... && browser shot ...'
```

## Notes

- **Selector support**: any CSS selector `document.querySelector` accepts.
  Complex selectors can be quoted: `browser click 0 'form input[name="q"]'`.
- **eval is the escape hatch**: for anything not covered (wait for element,
  extract structured data, read localStorage), pass raw JS. It runs with
  `returnByValue` + `awaitPromise`, so `await fetch(...)` works.
- **Frames/iframes**: `Runtime.evaluate` runs in the top frame only. For
  iframe content use `browser eval` with
  `document.querySelector('iframe').contentDocument...`.
- **SPA timing**: after navigation, wait before interacting:
  `browser wait 1500` or poll with `browser eval` until a selector exists.
- **CAPTCHA / hard walls** (topromax-ops §10): keep the Chrome window visible
  and let a human complete the step, then continue.
