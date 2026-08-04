package tools

import (
	"os"
	"strings"
	"testing"
)

func TestDiffLinesSimple(t *testing.T) {
	old := "line1\nline2\nline3\nline4\n"
	new := "line1\nline2 changed\nline3\nline4\n"
	got := DiffLines(old, new, 4)
	if !strings.Contains(got, "-2 line2") || !strings.Contains(got, "+2 line2 changed") {
		t.Errorf("expected removed/added lines with numbers:\n%s", got)
	}
	if !strings.Contains(got, " 1 line1") || !strings.Contains(got, " 3 line3") {
		t.Errorf("expected context lines:\n%s", got)
	}
}

func TestDiffLinesMultiLineInsert(t *testing.T) {
	old := "a\nb\nc\n"
	new := "a\nb\nX\nY\nZ\nc\n"
	got := DiffLines(old, new, 4)
	for _, want := range []string{"+3 X", "+4 Y", "+5 Z"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestDiffLinesContextTrimming(t *testing.T) {
	// 20 identical lines around a change: only 4 context lines each side + "..." .
	oldLines := make([]string, 21)
	newLines := make([]string, 21)
	for i := range oldLines {
		oldLines[i] = "same"
		newLines[i] = "same"
	}
	oldLines[10] = "old"
	newLines[10] = "new"
	got := DiffLines(strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"), 4)
	if strings.Count(got, "same") > 10 {
		t.Errorf("context not trimmed (got %d same lines):\n%s", strings.Count(got, "same"), got)
	}
	if !strings.Contains(got, "...") {
		t.Errorf("expected skip marker:\n%s", got)
	}
}

func TestPreviewEdit(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/f.txt"
	if err := writeFile(path, "one\ntwo\nthree\n"); err != nil {
		t.Fatal(err)
	}
	diff, err := PreviewEdit(path, "two", "TWO")
	if err != nil {
		t.Fatalf("PreviewEdit: %v", err)
	}
	if !strings.Contains(diff, "-2 two") || !strings.Contains(diff, "+2 TWO") {
		t.Errorf("unexpected diff:\n%s", diff)
	}
	// File must be untouched.
	data, _ := os.ReadFile(path)
	if string(data) != "one\ntwo\nthree\n" {
		t.Errorf("PreviewEdit modified the file: %q", data)
	}
	// Missing oldText → error, file untouched.
	if _, err := PreviewEdit(path, "nope", "x"); err == nil {
		t.Error("expected error for missing oldText")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// TestBashToolEnv verifies session metadata (PI_SESSION_ID etc.) set via
// Env is visible to the executed command.
func TestBashToolEnv(t *testing.T) {
	b := &BashTool{
		Env: map[string]string{
			"PI_SESSION_ID":   "abc123",
			"PI_MODEL":        "deepseek-v4-flash",
			"PI_SESSION_FILE": "/tmp/session.jsonl",
		},
	}
	res := b.Execute(map[string]interface{}{
		"command": `echo "id=$PI_SESSION_ID model=$PI_MODEL file=$PI_SESSION_FILE"`,
		"timeout": 10,
	})
	if !res.Success {
		t.Fatalf("bash failed: %s", res.Error)
	}
	want := "id=abc123 model=deepseek-v4-flash file=/tmp/session.jsonl"
	if strings.TrimSpace(res.Output) != want {
		t.Errorf("env not propagated: got %q, want %q", res.Output, want)
	}
}

// TestEnvToSlice verifies the map→"KEY=VALUE" conversion used for cmd.Env.
func TestEnvToSlice(t *testing.T) {
	got := envToSlice(map[string]string{"A": "1", "B": "x y"})
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %v", got)
	}
	for _, kv := range got {
		if kv != "A=1" && kv != "B=x y" {
			t.Errorf("unexpected entry: %q", kv)
		}
	}
}
