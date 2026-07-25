package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func (r *Registry) Register(t Tool)         { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) Tool     { return r.tools[name] }
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

// ─── BashTool ────────────────────────────────────────────────────

type BashTool struct{}

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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()
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

func (t *GrepTool) Name() string        { return "grep" }
func (t *GrepTool) Description() string { return "Search for a pattern in files. Uses ripgrep if available, grep otherwise." }

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
	// grep returns exit code 1 for "no matches"
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && len(output) == 0 {
			return &Result{Success: true, Output: "No matches found."}
		}
		if len(output) > 0 {
			return &Result{Success: true, Output: outStr}
		}
		return &Result{Success: false, Output: outStr, Error: err.Error()}
	}
	return &Result{Success: true, Output: outStr}
}

// ─── FindTool ────────────────────────────────────────────────────

// FindTool finds files by name pattern.
type FindTool struct{}

func (t *FindTool) Name() string        { return "find" }
func (t *FindTool) Description() string { return "Find files by name pattern. Uses fd if available, find otherwise." }

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
	if err != nil {
		if len(output) > 0 {
			return &Result{Success: true, Output: outStr}
		}
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
