package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Result struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

type Tool interface {
	Name() string
	Description() string
	Schema() map[string]interface{}
	Execute(input map[string]interface{}) *Result
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool)      { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) Tool { return r.tools[name] }
func (r *Registry) List() []Tool {
	var out []Tool
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ─── ReadTool ────────────────────────────────────────────────────

type ReadTool struct{}

func (t *ReadTool) Name() string        { return "read" }
func (t *ReadTool) Description() string { return "Read file contents. Supports text files and images." }

func (t *ReadTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":   map[string]interface{}{"type": "string", "description": "File path (relative or absolute)"},
			"offset": map[string]interface{}{"type": "number", "description": "Line number to start from (1-indexed)"},
			"limit":  map[string]interface{}{"type": "number", "description": "Maximum lines to read"},
		},
		"required": []string{"path"},
	}
}

func (t *ReadTool) Execute(input map[string]interface{}) *Result {
	path, _ := input["path"].(string)
	if path == "" {
		return &Result{Error: "path required"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &Result{Error: err.Error()}
	}
	output := string(data)
	lines := strings.Split(output, "\n")

	offset := 1
	if v, ok := input["offset"].(float64); ok {
		offset = int(v)
	}
	if offset < 1 {
		offset = 1
	}
	limit := 0
	if v, ok := input["limit"].(float64); ok {
		limit = int(v)
	}

	start := offset - 1
	if start >= len(lines) {
		return &Result{Error: fmt.Sprintf("offset %d exceeds %d lines", offset, len(lines))}
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	output = strings.Join(lines[start:end], "\n")
	if len(output) > 50000 {
		output = output[:50000] + "\n\n[Truncated]"
	}
	return &Result{Success: true, Output: output}
}

// ─── WriteTool ───────────────────────────────────────────────────

type WriteTool struct{}

func (t *WriteTool) Name() string        { return "write" }
func (t *WriteTool) Description() string { return "Create or overwrite a file." }

func (t *WriteTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":    map[string]interface{}{"type": "string", "description": "File path"},
			"content": map[string]interface{}{"type": "string", "description": "File content"},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteTool) Execute(input map[string]interface{}) *Result {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	if path == "" {
		return &Result{Error: "path required"}
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return &Result{Error: err.Error()}
	}
	return &Result{Success: true, Output: fmt.Sprintf("Wrote %d bytes to %s", len(content), path)}
}

// ─── EditTool ────────────────────────────────────────────────────

type EditTool struct{}

func (t *EditTool) Name() string        { return "edit" }
func (t *EditTool) Description() string { return "Edit a file using exact text replacement." }

func (t *EditTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path":    map[string]interface{}{"type": "string", "description": "File to edit"},
			"oldText": map[string]interface{}{"type": "string", "description": "Exact text to replace"},
			"newText": map[string]interface{}{"type": "string", "description": "Replacement text"},
		},
		"required": []string{"path", "oldText", "newText"},
	}
}

func (t *EditTool) Execute(input map[string]interface{}) *Result {
	path, _ := input["path"].(string)
	oldText, _ := input["oldText"].(string)
	newText, _ := input["newText"].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		return &Result{Error: err.Error()}
	}
	content := string(data)
	if !strings.Contains(content, oldText) {
		return &Result{Error: "oldText not found"}
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return &Result{Error: err.Error()}
	}
	return &Result{Success: true, Output: fmt.Sprintf("Replaced in %s", path)}
}

// ─── Edit diff preview (pi-style line diff) ────────────────────

// PreviewEdit computes a line-level diff of applying oldText→newText to the
// file at path, WITHOUT modifying it. The returned text uses pi's display
// format: "+<lineno> content" / "-<lineno> content" / " <lineno> context"
// (4 lines around each change) and "..." for skipped runs. Callers render
// colors themselves.
func PreviewEdit(path, oldText, newText string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	if !strings.Contains(content, oldText) {
		return "", fmt.Errorf("oldText not found")
	}
	newContent := strings.Replace(content, oldText, newText, 1)
	return DiffLines(content, newContent, 4), nil
}

// DiffLines renders a display-oriented line diff between two texts.
// Format mirrors pi's generateDiffString: line numbers prefixed with
// '+', '-' or ' ' (context), '...' for skipped context runs.
func DiffLines(oldContent, newContent string, contextLines int) string {
	oldLines := strings.Split(strings.ReplaceAll(oldContent, "\r\n", "\n"), "\n")
	newLines := strings.Split(strings.ReplaceAll(newContent, "\r\n", "\n"), "\n")
	// Trailing split artifact.
	if len(oldLines) > 0 && oldLines[len(oldLines)-1] == "" {
		oldLines = oldLines[:len(oldLines)-1]
	}
	if len(newLines) > 0 && newLines[len(newLines)-1] == "" {
		newLines = newLines[:len(newLines)-1]
	}

	ops := lineDiffOps(oldLines, newLines)

	lineNumWidth := len(fmt.Sprintf("%d", maxInt(len(oldLines), len(newLines))))
	out := make([]string, 0, len(ops))

	ctxLine := func(n int, text string) string {
		return " " + fmt.Sprintf("%*d", lineNumWidth, n) + " " + text
	}
	chgLine := func(kind byte, n int, text string) string {
		return string(kind) + fmt.Sprintf("%*d", lineNumWidth, n) + " " + text
	}

	// Group ops into runs: context runs alternate with change runs.
	var runs [][]lineDiffOp
	var cur []lineDiffOp
	for _, op := range ops {
		if len(cur) > 0 && ((cur[0].kind == ' ') != (op.kind == ' ')) {
			runs = append(runs, cur)
			cur = nil
		}
		cur = append(cur, op)
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}

	for ri, run := range runs {
		if run[0].kind == ' ' {
			// Context run: show context around neighbouring changes only
			// (pi semantics: isolated context runs are skipped entirely).
			hasLeading := ri > 0
			hasTrailing := ri < len(runs)-1
			if !hasLeading && !hasTrailing {
				continue
			}
			if hasLeading && hasTrailing {
				if len(run) <= contextLines*2 {
					for _, op := range run {
						out = append(out, ctxLine(op.oldLine, op.text))
					}
				} else {
					for _, op := range run[:contextLines] {
						out = append(out, ctxLine(op.oldLine, op.text))
					}
					out = append(out, " "+strings.Repeat(" ", lineNumWidth)+" ...")
					for _, op := range run[len(run)-contextLines:] {
						out = append(out, ctxLine(op.oldLine, op.text))
					}
				}
			} else if hasLeading {
				// Trailing context after the last change: skip-marker + tail.
				if len(run) > contextLines {
					out = append(out, " "+strings.Repeat(" ", lineNumWidth)+" ...")
				}
				for _, op := range run[maxInt(0, len(run)-contextLines):] {
					out = append(out, ctxLine(op.oldLine, op.text))
				}
			} else {
				// Leading context before the first change: head + skip-marker.
				for _, op := range run[:minInt(contextLines, len(run))] {
					out = append(out, ctxLine(op.oldLine, op.text))
				}
				if len(run) > contextLines {
					out = append(out, " "+strings.Repeat(" ", lineNumWidth)+" ...")
				}
			}
			continue
		}
		// Change run: removed lines (old numbers) then added lines (new numbers).
		for _, op := range run {
			switch op.kind {
			case '-':
				out = append(out, chgLine('-', op.oldLine, op.text))
			case '+':
				out = append(out, chgLine('+', op.newLine, op.text))
			}
		}
	}
	return strings.Join(out, "\n")
}

// lineDiffOp is one line-level diff operation.
type lineDiffOp struct {
	kind    byte // ' ' keep, '-' remove, '+' add
	text    string
	oldLine int // 1-based line in the old file (0 = n/a)
	newLine int // 1-based line in the new file (0 = n/a)
}

// lineDiffOps computes line-level LCS diff between a and b, pairing
// removals and additions into adjacent runs.
func lineDiffOps(a, b []string) []lineDiffOp {
	// LCS table (int instead of bool to reconstruct).
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var ops []lineDiffOp
	i, j, oi, nj := 0, 0, 1, 1
	for i < n && j < m {
		if a[i] == b[j] {
			ops = append(ops, lineDiffOp{' ', a[i], oi, nj})
			i, j, oi, nj = i+1, j+1, oi+1, nj+1
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, lineDiffOp{'-', a[i], oi, 0})
			i, oi = i+1, oi+1
		} else {
			ops = append(ops, lineDiffOp{'+', b[j], 0, nj})
			j, nj = j+1, nj+1
		}
	}
	for i < n {
		ops = append(ops, lineDiffOp{'-', a[i], oi, 0})
		i, oi = i+1, oi+1
	}
	for j < m {
		ops = append(ops, lineDiffOp{'+', b[j], 0, nj})
		j, nj = j+1, nj+1
	}
	return ops
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── BashTool ────────────────────────────────────────────────────

// ANSI colors for the bash countdown display.
const (
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiGray   = "\033[90m"
	ansiReset  = "\033[0m"
)

// BashTool executes a bash command with a timeout.
// Set Ctx to the agent's run context so ESC/Ctrl+C can kill long commands.
type BashTool struct {
	Ctx context.Context
}

func (t *BashTool) Name() string        { return "bash" }
func (t *BashTool) Description() string { return "Execute a bash command." }

func (t *BashTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{"type": "string", "description": "Bash command to execute"},
			"timeout": map[string]interface{}{"type": "number", "description": "Timeout in seconds"},
		},
		"required": []string{"command"},
	}
}

func (t *BashTool) Execute(input map[string]interface{}) *Result {
	command, _ := input["command"].(string)
	if command == "" {
		return &Result{Error: "command required"}
	}
	timeoutSec := 30
	if v, ok := input["timeout"].(float64); ok && v > 0 {
		timeoutSec = int(v)
	}
	baseCtx := t.Ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	// Run the child in its own process group so a timeout can kill the
	// entire tree (bash + grandchildren). Without this, orphaned children
	// inherit the stdout pipe and cmd.Wait() blocks until they exit —
	// hanging the agent for minutes (e.g. `bash -c "echo hi; sleep 30"`).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Kill the whole process group when the context fires (timeout/cancel).
	// Go's CommandContext only SIGKILLs the direct child.
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()

	// Live countdown: after a short grace period, show the remaining seconds
	// until timeout on stderr, updating every second. Cleared on completion
	// so the ✓/✗ status renders on a clean line.
	var wg sync.WaitGroup
	done := make(chan struct{})
	shown := make(chan struct{})
	if timeoutSec >= 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			// Grace period so fast commands don't flicker a status line.
			select {
			case <-done:
				return
			case <-time.After(3 * time.Second):
			}
			printTick := func(first bool) {
				remaining := timeoutSec - int(time.Since(start).Seconds())
				if remaining < 0 {
					remaining = 0
				}
				color := ansiGreen
				switch {
				case remaining <= 5:
					color = ansiRed
				case remaining <= 10:
					color = ansiYellow
				}
				prefix := "\r\x1b[2K"
				if first {
					prefix = "\n"
				}
				fmt.Fprintf(os.Stderr, "%s%s⏳ %s执行中 %s%2ds%s/%ds 后超时%s",
					prefix, ansiGray, ansiReset, color, remaining, ansiReset, timeoutSec, ansiReset)
			}
			printTick(true)
			close(shown)
			for {
				select {
				case <-done:
					return
				case <-time.After(time.Second):
					printTick(false)
				}
			}
		}()
	}

	output, err := cmd.CombinedOutput()
	close(done)
	wg.Wait()

	// Erase the countdown line so the ✓/✗ status and output render cleanly.
	select {
	case <-shown:
		fmt.Fprint(os.Stderr, "\r\x1b[2K")
	default:
	}

	outStr := string(output)
	if len(outStr) > 50000 {
		outStr = outStr[:50000] + "\n\n[Truncated]"
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{Success: false, Output: outStr, Error: fmt.Sprintf("timed out after %ds", timeoutSec)}
		}
		return &Result{Success: false, Output: outStr, Error: err.Error()}
	}
	return &Result{Success: true, Output: outStr}
}

// ─── GrepTool ────────────────────────────────────────────────────

// GrepTool searches files for a pattern.
type GrepTool struct{}

func (t *GrepTool) Name() string { return "grep" }
func (t *GrepTool) Description() string {
	return "Search for a pattern in files. Uses ripgrep if available, grep otherwise."
}

func (t *GrepTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{"type": "string", "description": "Pattern to search for (regex supported)"},
			"path":    map[string]interface{}{"type": "string", "description": "Directory or file to search in (default: current directory)"},
			"include": map[string]interface{}{"type": "string", "description": "File pattern to include (e.g. '*.go')"},
		},
		"required": []string{"pattern"},
	}
}

func (t *GrepTool) Execute(input map[string]interface{}) *Result {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return &Result{Error: "pattern required"}
	}
	searchPath, _ := input["path"].(string)
	if searchPath == "" {
		searchPath = "."
	}
	include, _ := input["include"].(string)

	// Prefer ripgrep if available
	rgPath, _ := exec.LookPath("rg")
	var cmd *exec.Cmd
	if rgPath != "" {
		args := []string{"--no-heading", "-n", "--color=never", "-e", pattern, searchPath}
		if include != "" {
			args = append(args, "--glob", include)
		}
		cmd = exec.Command("rg", args...)
	} else {
		args := []string{"-rn", "-E", "-e", pattern, searchPath}
		if include != "" {
			args = append(args, "--include="+include)
		}
		cmd = exec.Command("grep", args...)
	}

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if len(outStr) > 50000 {
		outStr = outStr[:50000] + "\n\n[Truncated]"
	}
	// Exit code 1 = "no matches" (rg/grep convention); anything else (2) is a
	// real error (bad path, invalid pattern) and must NOT be reported as a
	// successful search.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return &Result{Success: true, Output: "No matches found."}
		}
		return &Result{Success: false, Output: outStr, Error: err.Error()}
	}
	return &Result{Success: true, Output: outStr}
}

// ─── FindTool ────────────────────────────────────────────────────

// FindTool finds files by name pattern.
type FindTool struct{}

func (t *FindTool) Name() string { return "find" }
func (t *FindTool) Description() string {
	return "Find files by name pattern. Uses fd if available, find otherwise."
}

func (t *FindTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{"type": "string", "description": "File name pattern (glob, e.g. '*.go')"},
			"path":    map[string]interface{}{"type": "string", "description": "Directory to search in (default: current directory)"},
		},
		"required": []string{"pattern"},
	}
}

func (t *FindTool) Execute(input map[string]interface{}) *Result {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return &Result{Error: "pattern required"}
	}
	searchPath, _ := input["path"].(string)
	if searchPath == "" {
		searchPath = "."
	}

	// Prefer fd if available
	fdPath, _ := exec.LookPath("fd")
	var cmd *exec.Cmd
	if fdPath != "" {
		cmd = exec.Command("fd", "--color=never", "--hidden", "--no-ignore", pattern, searchPath)
	} else {
		cmd = exec.Command("find", searchPath, "-name", pattern)
	}

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	if len(outStr) > 50000 {
		outStr = outStr[:50000] + "\n\n[Truncated]"
	}
	// fd exits 1 on error (bad path); find exits 1 on error too. Never report
	// an error message as a successful result.
	if err != nil {
		return &Result{Success: false, Output: outStr, Error: err.Error()}
	}
	if outStr == "" {
		return &Result{Success: true, Output: "No files found."}
	}
	return &Result{Success: true, Output: outStr}
}

// ─── LsTool ──────────────────────────────────────────────────────

// LsTool lists files in a directory.
type LsTool struct{}

func (t *LsTool) Name() string        { return "ls" }
func (t *LsTool) Description() string { return "List files in a directory." }

func (t *LsTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "Directory path (default: current directory)"},
		},
		"required": []string{},
	}
}

func (t *LsTool) Execute(input map[string]interface{}) *Result {
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return &Result{Error: err.Error()}
	}
	var out strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			fmt.Fprintf(&out, "%s/\n", e.Name())
		} else {
			info, _ := e.Info()
			size := ""
			if info != nil {
				s := info.Size()
				if s < 1024 {
					size = fmt.Sprintf(" (%dB)", s)
				} else if s < 1024*1024 {
					size = fmt.Sprintf(" (%dK)", s/1024)
				} else {
					size = fmt.Sprintf(" (%dM)", s/(1024*1024))
				}
			}
			fmt.Fprintf(&out, "%s%s\n", e.Name(), size)
		}
	}
	result := out.String()
	if result == "" {
		result = "(empty directory)"
	}
	return &Result{Success: true, Output: result}
}
