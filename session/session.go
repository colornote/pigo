// Package session implements JSONL-based session persistence
// inspired by pi's session format with tree-structured branching.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ─── Session Entry ─────────────────────────────────────────────────

// Entry represents one message in a session.
// Each entry has a unique ID and a parentId, enabling tree-structured branching.
type Entry struct {
	ID        string `json:"id"`
	ParentID  string `json:"parentId,omitempty"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Timestamp string `json:"timestamp"`
}

// ─── Session ───────────────────────────────────────────────────────

// Session holds in-memory state for an active session.
type Session struct {
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	Entries   []Entry `json:"-"`       // in-memory, not serialized to meta
	FilePath  string  `json:"-"`       // JSONL file path
	MetaPath  string  `json:"-"`       // sidecar meta file
	Project   string  `json:"project"` // slug for project
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// sessionMeta is stored in a sidecar file next to the JSONL.
type sessionMeta struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	CreatedAt string `json:"created_at"`
}

// ─── Manager ───────────────────────────────────────────────────────

// Manager handles session persistence and discovery.
type Manager struct {
	rootDir string // ~/.pigo/sessions
}

// NewManager creates a session manager rooted at dir (default: ~/.pigo/sessions).
func NewManager(rootDir string) *Manager {
	os.MkdirAll(rootDir, 0755)
	return &Manager{rootDir: rootDir}
}

// ProjectDir returns the directory for a project's sessions.
func (m *Manager) ProjectDir(cwd string) string {
	return filepath.Join(m.rootDir, projectSlug(cwd))
}

// Create starts a new session with an optional name.
func (m *Manager) Create(cwd, name string) (*Session, error) {
	id := newSessionID()
	now := time.Now().UTC().Format(time.RFC3339)

	pDir := m.ProjectDir(cwd)
	os.MkdirAll(pDir, 0755)

	fp := filepath.Join(pDir, id+".jsonl")
	mp := filepath.Join(pDir, id+".meta.json")

	s := &Session{
		ID:        id,
		Name:      name,
		FilePath:  fp,
		MetaPath:  mp,
		Project:   projectSlug(cwd),
		CreatedAt: now,
		UpdatedAt: now,
		Entries:   nil,
	}

	// Write meta sidecar
	if err := s.WriteMeta(); err != nil {
		return nil, err
	}

	// Write empty session (creates the file)
	if err := s.Flush(); err != nil {
		return nil, err
	}
	return s, nil
}

// Load reads a session from its JSONL file.
func (m *Manager) Load(filePath string) (*Session, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}

	var entries []Entry
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	// Derive session ID from filename
	base := filepath.Base(filePath)
	id := strings.TrimSuffix(base, ".jsonl")
	mp := filepath.Join(filepath.Dir(filePath), id+".meta.json")

	s := &Session{
		ID:       id,
		FilePath: filePath,
		MetaPath: mp,
		Entries:  entries,
	}

	if len(entries) > 0 {
		s.CreatedAt = entries[0].Timestamp
		s.UpdatedAt = entries[len(entries)-1].Timestamp
	}

	// Load meta sidecar if exists (overrides timestamps with persisted values)
	s.readMeta()

	return s, nil
}

// LoadByID finds a session by ID prefix and loads it.
func (m *Manager) LoadByID(cwd, idPrefix string) (*Session, error) {
	pDir := m.ProjectDir(cwd)
	entries, err := os.ReadDir(pDir)
	if err != nil {
		return nil, fmt.Errorf("no sessions for project")
	}

	// 1. Search .jsonl files matching prefix
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), idPrefix) && strings.HasSuffix(e.Name(), ".jsonl") {
			return m.Load(filepath.Join(pDir, e.Name()))
		}
	}

	// 2. Search subdirectories (old format: project-dir/session-id.jsonl)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		subDir := filepath.Join(pDir, e.Name())
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if strings.HasPrefix(se.Name(), idPrefix) && strings.HasSuffix(se.Name(), ".jsonl") {
				return m.Load(filepath.Join(subDir, se.Name()))
			}
		}
	}

	// 3. Match project directory name itself (e.g., the hash)
	if strings.HasPrefix(filepath.Base(pDir), idPrefix) {
		s, err := m.Latest(cwd)
		if err == nil {
			return s, nil
		}
	}

	return nil, fmt.Errorf("session '%s' not found. Use /resume or -r to browse", idPrefix)
}

// Latest returns the most recently updated session for a project.
func (m *Manager) Latest(cwd string) (*Session, error) {
	pDir := m.ProjectDir(cwd)
	entries, err := os.ReadDir(pDir)
	if err != nil {
		return nil, fmt.Errorf("no sessions found")
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no sessions found")
	}

	// Sort by modification time descending
	sort.Slice(entries, func(i, j int) bool {
		infoI, _ := entries[i].Info()
		infoJ, _ := entries[j].Info()
		return infoI.ModTime().After(infoJ.ModTime())
	})

	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			return m.Load(filepath.Join(pDir, e.Name()))
		}
	}
	return nil, fmt.Errorf("no sessions found")
}

// List returns all sessions for a project, sorted by modified time descending.
func (m *Manager) List(cwd string) ([]Session, error) {
	pDir := m.ProjectDir(cwd)
	entries, err := os.ReadDir(pDir)
	if err != nil {
		return nil, nil // no sessions yet
	}

	var sessions []Session
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		s, err := m.Load(filepath.Join(pDir, e.Name()))
		if err != nil {
			continue
		}
		sessions = append(sessions, *s)
	}

	// Sort by updated desc
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})

	return sessions, nil
}

// ─── Session Methods ───────────────────────────────────────────────

// AddEntry appends a message entry and flushes to disk.
func (s *Session) AddEntry(parentID, role, content string, toolUseID string) error {
	e := Entry{
		ID:        newEntryID(),
		ParentID:  parentID,
		Role:      role,
		Content:   content,
		ToolUseID: toolUseID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	s.Entries = append(s.Entries, e)
	s.UpdatedAt = e.Timestamp
	return s.Flush()
}

// Flush writes all entries to the JSONL file.
func (s *Session) Flush() error {
	os.MkdirAll(filepath.Dir(s.FilePath), 0755)

	var lines []string
	for _, e := range s.Entries {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		lines = append(lines, string(b))
	}
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(s.FilePath, []byte(content), 0644)
}

// WriteMeta persists session metadata to the sidecar JSON file.
func (s *Session) WriteMeta() error {
	m := sessionMeta{
		ID:        s.ID,
		Name:      s.Name,
		CreatedAt: s.CreatedAt,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.MetaPath, b, 0644)
}

// readMeta loads session metadata from the sidecar file.
func (s *Session) readMeta() {
	data, err := os.ReadFile(s.MetaPath)
	if err != nil {
		return
	}
	var m sessionMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	s.Name = m.Name
	if m.CreatedAt != "" {
		s.CreatedAt = m.CreatedAt
	}
}

// LastID returns the ID of the last entry, or empty.
func (s *Session) LastID() string {
	if len(s.Entries) == 0 {
		return ""
	}
	return s.Entries[len(s.Entries)-1].ID
}

// Count returns the number of entries.
func (s *Session) Count() int {
	return len(s.Entries)
}

// Summary returns a one-line summary for list displays.
func (s *Session) Summary() string {
	name := s.Name
	if name == "" {
		name = s.ID
	}
	msgCount := len(s.Entries)
	firstMsg := ""
	if msgCount > 0 {
		firstMsg = s.Entries[0].Content
		if len(firstMsg) > 60 {
			firstMsg = firstMsg[:57] + "..."
		}
	}
	ts := s.UpdatedAt
	if len(ts) > 19 {
		ts = ts[:19]
	}
	return fmt.Sprintf("%s | %d msgs | %s | %s",
		name, msgCount, ts, firstMsg)
}

// Messages returns entries converted to the format expected by agent's RunFromSession.
func (s *Session) Messages() []Entry {
	return s.Entries
}

// ─── Helpers ───────────────────────────────────────────────────────

func newSessionID() string {
	return randID(12)
}

func newEntryID() string {
	return randID(16)
}

func randID(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, n)
	for i := range result {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[idx.Int64()]
	}
	return string(result)
}

// projectSlug creates a filesystem-safe project identifier from the working directory.
func projectSlug(cwd string) string {
	h := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(h[:8])
}
