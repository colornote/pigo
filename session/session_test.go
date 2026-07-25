package session

import (
	"os"
	"testing"
)

func TestSessionCreate(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	s, err := m.Create("/test/project", "my-session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if s.ID == "" {
		t.Error("expected non-empty ID")
	}
	if s.Count() != 0 {
		t.Errorf("expected 0 entries, got %d", s.Count())
	}

	// Verify file exists
	if _, err := os.Stat(s.FilePath); os.IsNotExist(err) {
		t.Error("session file not created")
	}
}

func TestSessionAddAndLoad(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	cwd := "/test/project2"
	s, err := m.Create(cwd, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Add entries
	s.AddEntry("", "user", "Hello", "")
	s.AddEntry(s.LastID(), "assistant", "Hi there!", "")
	s.AddEntry(s.LastID(), "user", "Do stuff", "")

	if s.Count() != 3 {
		t.Errorf("expected 3 entries, got %d", s.Count())
	}

	// Load back
	loaded, err := m.Load(s.FilePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Count() != 3 {
		t.Errorf("loaded count: expected 3, got %d", loaded.Count())
	}

	if loaded.Entries[0].Content != "Hello" {
		t.Errorf("first entry: expected 'Hello', got '%s'", loaded.Entries[0].Content)
	}

	if loaded.Entries[1].Role != "assistant" {
		t.Errorf("second entry role: expected 'assistant', got '%s'", loaded.Entries[1].Role)
	}

	// Load by ID
	loaded2, err := m.LoadByID(cwd, s.ID[:4])
	if err != nil {
		t.Fatalf("LoadByID: %v", err)
	}
	if loaded2.ID != s.ID {
		t.Errorf("load by ID mismatch: %s vs %s", loaded2.ID, s.ID)
	}
}

func TestSessionLatest(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	cwd := "/test/project3"

	s1, _ := m.Create(cwd, "first")
	s1.AddEntry("", "user", "old", "")

	s2, _ := m.Create(cwd, "second")
	s2.AddEntry("", "user", "newer", "")

	latest, err := m.Latest(cwd)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}

	if latest.ID != s2.ID {
		t.Errorf("expected latest %s, got %s", s2.ID, latest.ID)
	}
}

func TestSessionList(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	cwd := "/test/project4"

	m.Create(cwd, "a")
	m.Create(cwd, "b")

	list, err := m.List(cwd)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}
}

func TestSessionNoExist(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	_, err := m.Latest("/nonexistent/project")
	if err == nil {
		t.Error("expected error for no sessions")
	}

	_, err = m.LoadByID("/nonexistent/project", "abc")
	if err == nil {
		t.Error("expected error for missing session")
	}
}

func TestSessionMessages(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	s, _ := m.Create("/test/project5", "")
	s.AddEntry("", "user", "msg1", "")
	s.AddEntry(s.LastID(), "assistant", "msg2", "")

	msgs := s.Messages()
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestSessionFlushEdgeCases(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	s, _ := m.Create("/test/project6", "")

	// Empty session flush
	if err := s.Flush(); err != nil {
		t.Errorf("flush empty: %v", err)
	}

	// Verify file is valid (empty or just newline)
	data, _ := os.ReadFile(s.FilePath)
	if len(data) > 1 {
		t.Logf("empty session file content: %q", string(data))
	}
}

func TestProjectSlug(t *testing.T) {
	s1 := projectSlug("/Users/test/my-project")
	s2 := projectSlug("/Users/test/my-project")
	s3 := projectSlug("/Users/test/other")

	if s1 != s2 {
		t.Errorf("same path produces different slugs: %s vs %s", s1, s2)
	}
	if s1 == s3 {
		t.Errorf("different paths produce same slug: %s", s1)
	}

	if len(s1) != 16 {
		t.Errorf("expected 16-char hex slug, got %d chars: %s", len(s1), s1)
	}
}
