//go:build darwin || linux

// browser — a Chrome-browser-like CLI for coding agents, driven over CDP.
//
// Attaches to a persistent Chrome profile on localhost:9222 (topromax-ops
// baseline, AGENT.md §11.1.1) with zero third-party dependencies.
//
// Usage:
//
//	browser doctor                        # preflight check + launch hint
//	browser open <url>                    # open new tab and navigate
//	browser tabs [filter]                 # list tabs (index, title, url)
//	browser eval <tab> '<js>'             # run JS, print result
//	browser click <tab> <selector>        # click first matching element
//	browser type <tab> <selector> <text>  # set input value + input event
//	browser text <tab> [selector]         # page/element innerText
//	browser scroll <tab> top|bottom|by <px>
//	browser shot <tab> <file.png>         # screenshot to PNG
//	browser close <tab>                   # close tab
//	browser wait <ms>                     # sleep (helper for agent scripts)
//	browser lock -- <cmd...>              # run cmd under the CDP flock
//
// <tab> is a 0-based index from `tabs`, or a URL/title substring.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pigo/browser/internal/cdp"
)

const lockPath = "/tmp/topromax-cdp.lock" // topromax-ops CDP serialization

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]

	var err error
	switch cmd {
	case "doctor":
		err = doctor()
	case "open":
		need(rest, 1)
		err = openTab(rest[0])
	case "tabs":
		err = tabs(rest)
	case "eval":
		need(rest, 2)
		err = evalJS(rest[0], strings.Join(rest[1:], " "))
	case "click":
		need(rest, 2)
		err = click(rest[0], rest[1])
	case "type":
		need(rest, 3)
		err = typeText(rest[0], rest[1], strings.Join(rest[2:], " "))
	case "text":
		err = pageText(rest)
	case "scroll":
		err = scroll(rest)
	case "shot":
		need(rest, 2)
		err = screenshot(rest[0], rest[1])
	case "close":
		need(rest, 1)
		err = closeTab(rest[0])
	case "wait":
		need(rest, 1)
		err = waitMs(rest[0])
	case "lock":
		err = lockRun(rest)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "browser: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "browser:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`browser — CDP Chrome automation for coding agents (zero deps)

Usage:
  browser doctor                        preflight check + launch hint
  browser open <url>                    open new tab and navigate
  browser tabs [filter]                 list tabs (index, title, url)
  browser eval <tab> '<js>'             run JS, print result
  browser click <tab> <selector>        click first matching element
  browser type <tab> <sel> <text>       set input value + input event
  browser text <tab> [selector]         page/element innerText
  browser scroll <tab> top|bottom|by <px>
  browser shot <tab> <file.png>         screenshot to PNG
  browser close <tab>                   close tab
  browser wait <ms>                     sleep helper
  browser lock -- <cmd...>              run cmd under the CDP flock

<tab> is a 0-based index from `+"`browser tabs`"+`, or a URL/title substring.
Env: CDP_PORT (default 9222). Lock: `+lockPath+` (topromax-ops convention).
`)
}

func need(args []string, n int) {
	if len(args) < n {
		fmt.Fprintf(os.Stderr, "browser: expected %d argument(s), got %d\n", n, len(args))
		usage()
		os.Exit(2)
	}
}

func port() int {
	if v := os.Getenv("CDP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			return p
		}
	}
	return cdp.DefaultPort
}

// resolveTab maps a spec (index or URL/title substring) to a Tab.
func resolveTab(spec string) (*cdp.Tab, error) {
	tabs, err := cdp.ListTabs(port())
	if err != nil {
		return nil, err
	}
	if len(tabs) == 0 {
		return nil, fmt.Errorf("no tabs open — use `browser open <url>` first")
	}
	if idx, err := strconv.Atoi(spec); err == nil {
		if idx < 0 || idx >= len(tabs) {
			return nil, fmt.Errorf("tab index %d out of range (0..%d)", idx, len(tabs)-1)
		}
		return &tabs[idx], nil
	}
	var matches []cdp.Tab
	for _, t := range tabs {
		if strings.Contains(t.URL, spec) || strings.Contains(t.Title, spec) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no tab matches %q", spec)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("%d tabs match %q — use an index from `browser tabs`", len(matches), spec)
	}
}

// ─── commands ───────────────────────────────────────────────────

func doctor() error {
	p := port()
	tabs, err := cdp.ListTabs(p)
	if err != nil {
		fmt.Printf("✗ Chrome CDP :%d NOT reachable — %v\n", p, err)
		fmt.Println("\nStart Chrome with the persistent debug profile (topromax-ops AGENT.md §11.1.1):")
		fmt.Println("\n  lsof -ti :9222 | xargs -r kill -9 2>/dev/null")
		fmt.Println(`  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \`)
		fmt.Println("     --remote-debugging-port=9222 \\")
		fmt.Println("     --remote-allow-origins=* \\")
		fmt.Println(`     --user-data-dir="$HOME/.chrome-cdp-profile" \`)
		fmt.Println("     --no-first-run 2>/dev/null &")
		fmt.Println("\nThen verify: curl -s 127.0.0.1:9222/json/version")
		return nil
	}
	fmt.Printf("✓ Chrome CDP :%d reachable — %d tab(s)\n", p, len(tabs))
	for i, t := range tabs {
		title := t.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		fmt.Printf("  [%d] %s\n      %s\n", i, title, t.URL)
	}
	if _, err := os.Stat(lockPath); err == nil {
		fmt.Printf("⚠ lock %s exists (stale locks auto-release via flock)\n", lockPath)
	} else {
		fmt.Printf("✓ lock %s free\n", lockPath)
	}
	if np := os.Getenv("NO_PROXY"); np == "" {
		fmt.Println("⚠ NO_PROXY is empty — CDP websockets may route through a SOCKS proxy and hang. Export: NO_PROXY=localhost,127.0.0.1")
	} else {
		fmt.Printf("✓ NO_PROXY=%s\n", np)
	}
	return nil
}

func openTab(target string) error {
	tab, err := cdp.NewTab(port(), target)
	if err != nil {
		return err
	}
	fmt.Printf("opened [id=%s]\n%s\n", tab.ID, tab.URL)
	return nil
}

func tabs(filter []string) error {
	list, err := cdp.ListTabs(port())
	if err != nil {
		return err
	}
	f := ""
	if len(filter) > 0 {
		f = filter[0]
	}
	count := 0
	for i, t := range list {
		if f != "" && !strings.Contains(t.URL, f) && !strings.Contains(t.Title, f) {
			continue
		}
		count++
		fmt.Printf("[%d] %s\n    %s\n", i, t.Title, t.URL)
	}
	if count == 0 {
		fmt.Println("(no matching tabs)")
	}
	return nil
}

func evalJS(spec, expr string) error {
	tab, err := resolveTab(spec)
	if err != nil {
		return err
	}
	client, err := cdp.Connect(tab.WSURL)
	if err != nil {
		return err
	}
	defer client.Close()
	val, err := client.Evaluate(expr)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(val, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func click(spec, sel string) error {
	expr := fmt.Sprintf(`(()=>{const q=%q;const el=document.querySelector(q);`+
		`if(!el)throw new Error("selector not found: "+q);`+
		`el.scrollIntoView({block:"center"});el.click();return "clicked: "+q;})()`, sel)
	tab, err := resolveTab(spec)
	if err != nil {
		return err
	}
	client, err := cdp.Connect(tab.WSURL)
	if err != nil {
		return err
	}
	defer client.Close()
	val, err := client.Evaluate(expr)
	if err != nil {
		return err
	}
	fmt.Println(val)
	return nil
}

func typeText(spec, sel, text string) error {
	expr := fmt.Sprintf(`(()=>{const q=%q;const el=document.querySelector(q);`+
		`if(!el)throw new Error("selector not found: "+q);`+
		`el.focus();el.value=%q;`+
		`el.dispatchEvent(new Event("input",{bubbles:true}));`+
		`el.dispatchEvent(new Event("change",{bubbles:true}));`+
		`return "typed "+el.value.length+" chars into "+q;})()`, sel, text)
	tab, err := resolveTab(spec)
	if err != nil {
		return err
	}
	client, err := cdp.Connect(tab.WSURL)
	if err != nil {
		return err
	}
	defer client.Close()
	val, err := client.Evaluate(expr)
	if err != nil {
		return err
	}
	fmt.Println(val)
	return nil
}

func pageText(args []string) error {
	spec := args[0]
	expr := `document.body?document.body.innerText:""`
	if len(args) > 1 && args[1] != "" {
		expr = fmt.Sprintf(`(()=>{const el=document.querySelector(%q);`+
			`return el?el.innerText:"";})()`, args[1])
	}
	tab, err := resolveTab(spec)
	if err != nil {
		return err
	}
	client, err := cdp.Connect(tab.WSURL)
	if err != nil {
		return err
	}
	defer client.Close()
	val, err := client.Evaluate(expr)
	if err != nil {
		return err
	}
	if s, ok := val.(string); ok {
		fmt.Print(s)
		if !strings.HasSuffix(s, "\n") {
			fmt.Println()
		}
		return nil
	}
	fmt.Println(val)
	return nil
}

func scroll(args []string) error {
	if len(args) < 2 {
		need(args, 2)
	}
	spec, mode := args[0], args[1]
	var expr string
	switch mode {
	case "top":
		expr = `window.scrollTo(0,0);"scrolled to top"`
	case "bottom":
		expr = `window.scrollTo(0,document.body.scrollHeight);"scrolled to bottom"`
	case "by":
		if len(args) < 3 {
			need(args, 3)
		}
		px, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("scroll by: invalid px %q", args[2])
		}
		expr = fmt.Sprintf(`window.scrollBy(0,%d);"scrolled by %dpx"`, px, px)
	default:
		return fmt.Errorf("scroll: mode must be top|bottom|by, got %q", mode)
	}
	tab, err := resolveTab(spec)
	if err != nil {
		return err
	}
	client, err := cdp.Connect(tab.WSURL)
	if err != nil {
		return err
	}
	defer client.Close()
	val, err := client.Evaluate(expr)
	if err != nil {
		return err
	}
	fmt.Println(val)
	return nil
}

func screenshot(spec, outFile string) error {
	tab, err := resolveTab(spec)
	if err != nil {
		return err
	}
	client, err := cdp.Connect(tab.WSURL)
	if err != nil {
		return err
	}
	defer client.Close()
	png, err := client.Screenshot()
	if err != nil {
		return err
	}
	if err := os.WriteFile(outFile, png, 0644); err != nil {
		return err
	}
	fmt.Printf("saved %s (%d bytes)\n", outFile, len(png))
	return nil
}

func closeTab(spec string) error {
	tab, err := resolveTab(spec)
	if err != nil {
		return err
	}
	if err := cdp.CloseTab(port(), tab.ID); err != nil {
		return err
	}
	fmt.Printf("closed: %s\n", tab.URL)
	return nil
}

func waitMs(s string) error {
	ms, err := strconv.Atoi(s)
	if err != nil || ms < 0 {
		return fmt.Errorf("wait: invalid ms %q", s)
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return nil
}

// lockRun runs a command under the CDP flock (topromax-ops §11.1.2):
// serialize access to the shared Chrome profile across agents.
func lockRun(args []string) error {
	// Accept both `lock -- cmd...` and `lock cmd...`.
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return fmt.Errorf("lock: usage: browser lock -- <cmd...>")
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock: flock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}
