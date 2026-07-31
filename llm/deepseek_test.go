package llm

import (
	"strings"
	"testing"
)

// runProcessDelta feeds deltas through processDelta and returns what was
// routed to the reasoning and content builders.
func runProcessDelta(deltas ...string) (reasoning, content string) {
	var r, c strings.Builder
	inThink := false
	pending := ""
	d := &DeepSeekClient{}
	for _, delta := range deltas {
		d.processDelta(delta, &inThink, &pending, &r, &c, nil, nil)
	}
	// Flush any held-back partial tag, mirroring SendStreamWithContext.
	if pending != "" {
		if inThink {
			r.WriteString(pending)
		} else {
			c.WriteString(pending)
		}
	}
	return r.String(), c.String()
}

func TestProcessDeltaNoTags(t *testing.T) {
	// Content without any tags must flow entirely to content — this was
	// the original bug (empty opening tag swallowed everything).
	r, c := runProcessDelta("Hello, this is the answer.")
	if r != "" {
		t.Errorf("expected no reasoning, got %q", r)
	}
	if c != "Hello, this is the answer." {
		t.Errorf("expected full content, got %q", c)
	}
}

func TestProcessDeltaChineseTags(t *testing.T) {
	deltas := []string{
		" 回复Let me think carefully about this",
		" problem.  /回复",
		"The final answer is 42.",
	}
	r, c := runProcessDelta(deltas...)
	if !strings.Contains(r, "Let me think carefully about this problem.") {
		t.Errorf("reasoning missing content: %q", r)
	}
	if strings.Contains(r, "The final answer is 42.") {
		t.Errorf("answer leaked into reasoning: %q", r)
	}
	if !strings.Contains(c, "The final answer is 42.") {
		t.Errorf("answer missing from content: %q", c)
	}
	if strings.Contains(c, "Let me think") {
		t.Errorf("reasoning leaked into content: %q", c)
	}
}

func TestProcessDeltaHTMLTags(t *testing.T) {
	r, c := runProcessDelta(
		"<reply>thinking in html</reply>",
		"visible answer",
	)
	if !strings.Contains(r, "thinking in html") {
		t.Errorf("reasoning missing: %q", r)
	}
	if !strings.Contains(c, "visible answer") {
		t.Errorf("content missing: %q", c)
	}
}

func TestProcessDeltaSplitAcrossChunks(t *testing.T) {
	// Opening tag split across two chunks, then reasoning, then closing.
	r, c := runProcessDelta(
		" 回",
		"复deep reasoning chunk",
		" /回复",
		"answer part",
	)
	if !strings.Contains(r, "deep reasoning chunk") {
		t.Errorf("reasoning missing: %q", r)
	}
	if !strings.Contains(c, "answer part") {
		t.Errorf("content missing: %q", c)
	}
}

func TestProcessDeltaUnclosedTag(t *testing.T) {
	// Reasoning that never closes: everything after the open tag goes to
	// reasoning, but nothing is lost or panics.
	r, c := runProcessDelta(" 回复unfinished reasoning")
	if !strings.Contains(r, "unfinished reasoning") {
		t.Errorf("reasoning missing: %q", r)
	}
	if c != "" {
		t.Errorf("expected empty content, got %q", c)
	}
}
