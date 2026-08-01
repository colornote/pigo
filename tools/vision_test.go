package tools

import (
	"errors"
	"strings"
	"testing"
)

// TestVisionToolRequiresPath verifies path is mandatory.
func TestVisionToolRequiresPath(t *testing.T) {
	v := &VisionTool{}
	res := v.Execute(map[string]interface{}{})
	if res.Success {
		t.Error("expected failure for missing path")
	}
	if !strings.Contains(res.Error, "path required") {
		t.Errorf("unexpected error: %q", res.Error)
	}
}

// TestVisionToolNotConfigured verifies a nil Runner reports a clear error.
func TestVisionToolNotConfigured(t *testing.T) {
	v := &VisionTool{}
	res := v.Execute(map[string]interface{}{"path": "a.png"})
	if res.Success {
		t.Error("expected failure without a runner")
	}
	if !strings.Contains(res.Error, "vision model not configured") {
		t.Errorf("unexpected error: %q", res.Error)
	}
}

// TestVisionToolRunnerInvoked verifies the injected runner is called with
// path and prompt, and its text becomes the tool result.
func TestVisionToolRunnerInvoked(t *testing.T) {
	var gotPath, gotPrompt string
	v := &VisionTool{
		Runner: func(path, prompt string) (string, error) {
			gotPath, gotPrompt = path, prompt
			return "A login form with username and password fields.", nil
		},
	}
	res := v.Execute(map[string]interface{}{
		"path":   "/tmp/login.png",
		"prompt": "What fields are on this form?",
	})
	if !res.Success {
		t.Fatalf("execute failed: %s", res.Error)
	}
	if gotPath != "/tmp/login.png" || gotPrompt != "What fields are on this form?" {
		t.Errorf("runner args: path=%q prompt=%q", gotPath, gotPrompt)
	}
	if res.Output != "A login form with username and password fields." {
		t.Errorf("unexpected output: %q", res.Output)
	}
}

// TestVisionToolRunnerError verifies runner errors propagate.
func TestVisionToolRunnerError(t *testing.T) {
	v := &VisionTool{
		Runner: func(path, prompt string) (string, error) {
			return "", errors.New("vision API: boom")
		},
	}
	res := v.Execute(map[string]interface{}{"path": "x.png"})
	if res.Success {
		t.Error("expected failure")
	}
	if !strings.Contains(res.Error, "boom") {
		t.Errorf("unexpected error: %q", res.Error)
	}
}

// TestVisionToolSchema verifies the schema advertises path + prompt.
func TestVisionToolSchema(t *testing.T) {
	schema := (&VisionTool{}).Schema()
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema has no properties: %#v", schema)
	}
	if _, ok := props["path"]; !ok {
		t.Error("schema missing path property")
	}
	if _, ok := props["prompt"]; !ok {
		t.Error("schema missing prompt property")
	}
}
