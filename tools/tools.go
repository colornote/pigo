package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

func (r *Registry) List() []Tool {
	var out []Tool
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// ReadTool
type ReadTool struct{}

func (t *ReadTool) Name() string { return "read" }

func (t *ReadTool) Description() string { return "Read file contents. Supports text files." }

func (t *ReadTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read (relative or absolute)",
			},
			"offset": map[string]interface{}{
				"type":        "number",
				"description": "Line number to start reading from (1-indexed)",
			},
			"limit": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of lines to read",
			},
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

	// Apply offset (1-indexed)
	offset := 1
	if offsetVal, ok := input["offset"]; ok {
		switch v := offsetVal.(type) {
		case float64:
			offset = int(v)
		}
	}
	if offset < 1 {
		offset = 1
	}

	// Apply limit
	limit := 0
	if limitVal, ok := input["limit"]; ok {
		switch v := limitVal.(type) {
		case float64:
			limit = int(v)
		}
	}

	start := offset - 1
	if start >= len(lines) {
		return &Result{Error: fmt.Sprintf("offset %d exceeds file length (%d lines)", offset, len(lines))}
	}

	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	output = strings.Join(lines[start:end], "\n")
	if len(output) > 50000 {
		output = output[:50000] + "\n\n[Truncated...]"
	}

	return &Result{Success: true, Output: output}
}

// WriteTool
type WriteTool struct{}

func (t *WriteTool) Name() string { return "write" }

func (t *WriteTool) Description() string { return "Create or overwrite a file with content." }

func (t *WriteTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the file",
			},
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

	// Create parent directories
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return &Result{Error: fmt.Sprintf("cannot create directories: %v", err)}
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return &Result{Error: err.Error()}
	}

	return &Result{Success: true, Output: fmt.Sprintf("Wrote %d bytes to %s", len(content), path)}
}

// EditTool - simple exact text replacement
type EditTool struct{}

func (t *EditTool) Name() string { return "edit" }

func (t *EditTool) Description() string { return "Edit a file using exact text replacement." }

func (t *EditTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to edit",
			},
			"oldText": map[string]interface{}{
				"type":        "string",
				"description": "Exact text to replace",
			},
			"newText": map[string]interface{}{
				"type":        "string",
				"description": "Replacement text",
			},
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
		return &Result{Error: "oldText not found in file"}
	}

	newContent := strings.Replace(content, oldText, newText, 1)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return &Result{Error: err.Error()}
	}

	return &Result{Success: true, Output: fmt.Sprintf("Replaced 1 occurrence in %s", path)}
}

// BashTool
type BashTool struct{}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string { return "Execute a bash command." }

func (t *BashTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "Bash command to execute",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (optional)",
			},
		},
		"required": []string{"command"},
	}
}

func (t *BashTool) Execute(input map[string]interface{}) *Result {
	command, _ := input["command"].(string)
	if command == "" {
		return &Result{Error: "command required"}
	}

	// Parse optional timeout (default: 30 seconds)
	timeoutSeconds := 30
	if timeoutVal, ok := input["timeout"]; ok {
		switch v := timeoutVal.(type) {
		case float64:
			timeoutSeconds = int(v)
		case string:
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				timeoutSeconds = n
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()

	outStr := string(output)
	if len(outStr) > 50000 {
		outStr = outStr[:50000] + "\n\n[Truncated...]"
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{Success: false, Output: outStr, Error: fmt.Sprintf("command timed out after %d seconds", timeoutSeconds)}
		}
		return &Result{Success: false, Output: outStr, Error: err.Error()}
	}
	return &Result{Success: true, Output: outStr}
}

// GrepTool — search file contents with a regex pattern
type GrepTool struct{}

func (t *GrepTool) Name() string { return "grep" }

func (t *GrepTool) Description() string {
	return "Search for a regex pattern in files. Returns matching lines with file paths and line numbers."
}

func (t *GrepTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Regex pattern to search for",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "File or directory to search in (default: current directory)",
			},
			"include": map[string]interface{}{
				"type":        "string",
				"description": "File glob pattern to include (e.g. '*.go', '*.md')",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GrepTool) Execute(input map[string]interface{}) *Result {
	pattern, _ := input["pattern"].(string)
	searchPath, _ := input["path"].(string)
	include, _ := input["include"].(string)

	if pattern == "" {
		return &Result{Error: "pattern required"}
	}
	if searchPath == "" {
		searchPath = "."
	}

	args := []string{"-rn", "--color=never"}
	if include != "" {
		args = append(args, "--include", include)
	}
	args = append(args, pattern, searchPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grep", args...)
	output, err := cmd.CombinedOutput()

	outStr := string(output)
	// grep returns exit code 1 if no matches found
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return &Result{Error: "grep timed out"}
		}
		if len(outStr) == 0 {
			return &Result{Success: true, Output: "(no matches)"}
		}
		return &Result{Success: false, Output: outStr, Error: err.Error()}
	}

	if len(outStr) > 50000 {
		outStr = outStr[:50000] + "\n\n[Truncated...]"
	}
	if outStr == "" {
		outStr = "(no matches)"
	}

	return &Result{Success: true, Output: outStr}
}

// FindTool — find files by name
type FindTool struct{}

func (t *FindTool) Name() string { return "find" }

func (t *FindTool) Description() string {
	return "Find files matching a glob pattern."
}

func (t *FindTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern": map[string]interface{}{
				"type":        "string",
				"description": "Glob pattern to match (e.g. '*.go', '**/*_test.go')",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory to search in (default: current directory)",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *FindTool) Execute(input map[string]interface{}) *Result {
	pattern, _ := input["pattern"].(string)
	searchPath, _ := input["path"].(string)

	if pattern == "" {
		return &Result{Error: "pattern required"}
	}
	if searchPath == "" {
		searchPath = "."
	}

	var matches []string
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		// Skip hidden dirs & common VCS
		if info.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && base != "." && base != ".." {
				return filepath.SkipDir
			}
			if base == "node_modules" || base == "vendor" || base == "__pycache__" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		matched, err := filepath.Match(pattern, filepath.Base(path))
		if err != nil {
			return nil
		}
		if matched {
			rel, _ := filepath.Rel(searchPath, path)
			matches = append(matches, rel)
		}
		return nil
	})

	if err != nil {
		return &Result{Error: err.Error()}
	}

	if len(matches) == 0 {
		return &Result{Success: true, Output: "(no files found)"}
	}

	// Also try recursive matching for ** patterns
	// filepath.Match doesn't handle **, so do a simple check
	if strings.Contains(pattern, "**") {
		// Re-run without the filepath.Base restriction
		matches = nil
		filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				base := filepath.Base(path)
				if strings.HasPrefix(base, ".") && base != "." && base != ".." {
					return filepath.SkipDir
				}
				if base == "node_modules" || base == "vendor" || base == "__pycache__" || base == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(searchPath, path)
			matched, err := filepath.Match(pattern, rel)
			if err != nil {
				return nil
			}
			if matched {
				matches = append(matches, rel)
			}
			return nil
		})
	}

	outStr := strings.Join(matches, "\n")
	if len(outStr) > 50000 {
		outStr = outStr[:50000] + "\n\n[Truncated...]"
	}

	return &Result{Success: true, Output: fmt.Sprintf("%d files:\n%s", len(matches), outStr)}
}

// LSTool — list directory contents
type LSTool struct{}

func (t *LSTool) Name() string { return "ls" }

func (t *LSTool) Description() string {
	return "List directory contents."
}

func (t *LSTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Directory path to list (default: current directory)",
			},
		},
		"required": []string{},
	}
}

func (t *LSTool) Execute(input map[string]interface{}) *Result {
	dirPath, _ := input["path"].(string)
	if dirPath == "" {
		dirPath = "."
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return &Result{Error: err.Error()}
	}

	var lines []string
	for _, e := range entries {
		prefix := ""
		if e.IsDir() {
			prefix = "/"
		}
		info, err := e.Info()
		if err == nil {
			lines = append(lines, fmt.Sprintf("%s%s  (%s)", prefix, e.Name(), info.ModTime().Format("Jan 02 15:04")))
		} else {
			lines = append(lines, fmt.Sprintf("%s%s", prefix, e.Name()))
		}
	}

	outStr := strings.Join(lines, "\n")
	if len(outStr) > 50000 {
		outStr = outStr[:50000] + "\n\n[Truncated...]"
	}
	return &Result{Success: true, Output: outStr}
}
