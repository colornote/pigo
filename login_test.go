package main

import (
	"os"
	"path/filepath"
	"pigo/agent"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func readEnv(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}

// TestUpdateEnvFileAddKey verifies a new key is appended to an existing file.
func TestUpdateEnvFileAddKey(t *testing.T) {
	path := writeEnv(t, "# comment\nPIGO_MODEL=deepseek-v4-flash\n")
	if err := updateEnvFile(path, "DEEPSEEK_API_KEY", "sk-test123"); err != nil {
		t.Fatalf("updateEnvFile: %v", err)
	}
	got := readEnv(t, path)
	if !strings.Contains(got, "DEEPSEEK_API_KEY=sk-test123") {
		t.Errorf("key not added:\n%s", got)
	}
	if !strings.Contains(got, "# comment") {
		t.Errorf("comment lost:\n%s", got)
	}
	if !strings.Contains(got, "PIGO_MODEL=deepseek-v4-flash") {
		t.Errorf("existing key lost:\n%s", got)
	}
}

// TestUpdateEnvFileReplaceKey verifies an existing key is replaced in place
// and duplicates collapse.
func TestUpdateEnvFileReplaceKey(t *testing.T) {
	path := writeEnv(t, "DEEPSEEK_API_KEY=old\nDEEPSEEK_API_KEY=old2\nPIGO_MODEL=x\n")
	if err := updateEnvFile(path, "DEEPSEEK_API_KEY", "new-key"); err != nil {
		t.Fatalf("updateEnvFile: %v", err)
	}
	got := readEnv(t, path)
	if strings.Count(got, "DEEPSEEK_API_KEY") != 1 {
		t.Errorf("expected exactly one DEEPSEEK_API_KEY line:\n%s", got)
	}
	if !strings.Contains(got, "DEEPSEEK_API_KEY=new-key") {
		t.Errorf("key not replaced:\n%s", got)
	}
	if !strings.Contains(got, "PIGO_MODEL=x") {
		t.Errorf("unrelated key lost:\n%s", got)
	}
}

// TestUpdateEnvFileCreatesMissing verifies a new file is created with 0600.
func TestUpdateEnvFileCreatesMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", ".env") // dir doesn't exist yet
	if err := updateEnvFile(path, "DEEPSEEK_API_KEY", "sk-xyz"); err != nil {
		t.Fatalf("updateEnvFile: %v", err)
	}
	got := readEnv(t, path)
	if got != "DEEPSEEK_API_KEY=sk-xyz\n" {
		t.Errorf("unexpected content: %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}
}

// TestRemoveEnvKey verifies removal of a key line while keeping the rest.
func TestRemoveEnvKey(t *testing.T) {
	path := writeEnv(t, "DEEPSEEK_API_KEY=secret\nPIGO_MODEL=x\n")
	if err := removeEnvKey(path, "DEEPSEEK_API_KEY"); err != nil {
		t.Fatalf("removeEnvKey: %v", err)
	}
	got := readEnv(t, path)
	if strings.Contains(got, "DEEPSEEK_API_KEY") {
		t.Errorf("key still present:\n%s", got)
	}
	if !strings.Contains(got, "PIGO_MODEL=x") {
		t.Errorf("unrelated key lost:\n%s", got)
	}
}

// TestRemoveEnvKeyMissingFile verifies removal on a missing file is a no-op.
func TestRemoveEnvKeyMissingFile(t *testing.T) {
	if err := removeEnvKey(filepath.Join(t.TempDir(), "nope.env"), "DEEPSEEK_API_KEY"); err != nil {
		t.Errorf("expected no error for missing file, got %v", err)
	}
}

// TestVerifyProviderKeySkipsWithoutEndpoint verifies providers without a
// native endpoint bypass verification (no network).
func TestVerifyProviderKeySkipsWithoutEndpoint(t *testing.T) {
	p := agent.ProviderByID("deepseek")
	if p == nil {
		t.Fatal("expected deepseek provider")
	}
	// Empty DSBaseURL → skip verification, always ok.
	p2 := *p
	p2.DSBaseURL = ""
	ok, err := verifyProviderKey(&p2, "anything")
	if err != nil || !ok {
		t.Errorf("expected (true, nil), got (%v, %v)", ok, err)
	}
}
