package agent

import (
	"strings"
	"testing"
)

// stripANSI removes escape sequences so tests can assert on visible text.
func stripANSI(s string) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			j := strings.IndexByte(s[i:], 'm')
			if j >= 0 {
				i += j + 1
				continue
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func renderAll(md string) string {
	var sb strings.Builder
	m := NewMarkdownStream(func(s string) { sb.WriteString(s) })
	m.Write(md)
	m.Flush()
	return stripANSI(sb.String())
}

func TestMarkdownHeadings(t *testing.T) {
	got := renderAll("# Title\n\n## Section\n\n### Sub\n")
	if !strings.Contains(got, "# Title") || !strings.Contains(got, "## Section") || !strings.Contains(got, "### Sub") {
		t.Errorf("headings not rendered:\n%s", got)
	}
	if !strings.Contains(got, "────") {
		t.Errorf("H1 should get a divider:\n%s", got)
	}
}

func TestMarkdownInline(t *testing.T) {
	got := renderAll("hello **bold** and `code` and [link](https://x.dev)\n")
	for _, want := range []string{"hello", "bold", "code", "link"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "https://x.dev") {
		t.Errorf("link URL should be hidden:\n%s", got)
	}
}

func TestMarkdownCodeBlock(t *testing.T) {
	got := renderAll("before\n```go\nfunc main() {}\n```\nafter\n")
	for _, want := range []string{"before", "func main() {}", "after", "┌", "└"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownList(t *testing.T) {
	got := renderAll("- one\n- two\n1. first\n2. second\n")
	for _, want := range []string{"one", "two", "1.", "first", "2.", "second"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestMarkdownTable verifies table parsing and CJK-aware column alignment:
// every data row must be padded to the same display width, so the cell text
// of each column starts at the same offset.
func TestMarkdownTable(t *testing.T) {
	md := "| 模型 | 价格 |\n|------|------|\n| DeepSeek V4 Flash | 0.14 |\n| 中文模型 | 1.00 |\n"
	got := renderAll(md)
	for _, want := range []string{"模型", "价格", "DeepSeek V4 Flash", "中文模型", "0.14", "1.00"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	lines := strings.Split(got, "\n")
	// Find the two data rows and compare the column offset of the price cell.
	var priceOffsets []int
	for _, l := range lines {
		if strings.Contains(l, "DeepSeek V4 Flash") || strings.Contains(l, "中文模型") {
			priceOffsets = append(priceOffsets, strings.Index(l, "0.14")/1)
		}
	}
	if len(priceOffsets) < 2 {
		t.Fatalf("expected two data rows, got %d:\n%s", len(priceOffsets), got)
	}
	// The price cell must start at the same display column in both rows.
	off1 := displayOffsetOf(lines, "DeepSeek V4 Flash", "0.14")
	off2 := displayOffsetOf(lines, "中文模型", "1.00")
	if off1 != off2 {
		t.Errorf("price column misaligned: row1=%d row2=%d\n%s", off1, off2, got)
	}
}

// displayOffsetOf returns the display column where needle starts on the line
// containing cellText (CJK-aware).
func displayOffsetOf(lines []string, cellText, needle string) int {
	for _, l := range lines {
		if strings.Contains(l, cellText) {
			idx := strings.Index(l, needle)
			if idx >= 0 {
				return DisplayWidth(l[:idx])
			}
		}
	}
	return -1
}

func TestMarkdownStreamPartialLine(t *testing.T) {
	// A line split across chunks must not render until complete, and the
	// final flush must emit it.
	var sb strings.Builder
	m := NewMarkdownStream(func(s string) { sb.WriteString(s) })
	m.Write("hel")
	m.Write("lo\n")
	if !strings.Contains(stripANSI(sb.String()), "hello") {
		t.Errorf("completed line not rendered:\n%q", sb.String())
	}
	m.Write("tail")
	m.Flush()
	if !strings.Contains(stripANSI(sb.String()), "tail") {
		t.Errorf("flushed partial line missing:\n%q", sb.String())
	}
}
