package agent

import (
	"strings"
)

// ─── Markdown → ANSI terminal renderer ─────────────────────────
//
// A lightweight streaming markdown renderer tailored for terminal output:
// headings, blockquotes, lists, fenced code blocks, tables (CJK-aligned),
// and inline styles (**bold**, `code`, [links]). It renders line-by-line as
// chunks stream in (pi renders incrementally too), buffering partial lines
// until they complete so tables align correctly.
//
// Colors reuse the PiGo palette (cyan headings, gray structure, yellow
// inline code); tables are padded with CJK-aware display width math from
// tui.go so Chinese cells stay aligned.

// markdownStream is the streaming state machine. Feed it chunks with Write;
// it emits fully-rendered lines to out as soon as they complete.
type markdownStream struct {
	out       func(string)
	pending   string // partial last line (no trailing \n yet)
	inCode    bool   // inside a fenced code block
	codeLang  string
	codeLines []string // accumulated code block content
	inTable   bool
	tableRows [][]string
}

// NewMarkdownStream creates a streaming markdown renderer writing rendered
// ANSI text through out.
func NewMarkdownStream(out func(string)) *markdownStream {
	return &markdownStream{out: out}
}

// Write feeds a chunk of raw text; complete lines are rendered immediately.
func (m *markdownStream) Write(chunk string) {
	chunk = m.pending + chunk
	m.pending = ""

	for {
		idx := strings.IndexByte(chunk, '\n')
		if idx < 0 {
			m.pending = chunk
			return
		}
		line := strings.TrimRight(chunk[:idx], "\r")
		chunk = chunk[idx+1:]
		m.processLine(line)
	}
}

// Flush emits any buffered partial line and closes open blocks. Call once
// when the stream ends.
func (m *markdownStream) Flush() {
	if m.pending != "" {
		m.processLine(m.pending)
		m.pending = ""
	}
	m.flushTable()
	m.flushCode()
}

func (m *markdownStream) processLine(line string) {
	trimmed := strings.TrimSpace(line)

	// Fenced code block toggling.
	if strings.HasPrefix(trimmed, "```") {
		if !m.inCode {
			m.flushTable()
			m.inCode = true
			m.codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			m.codeLines = nil
		} else {
			m.flushCode()
		}
		return
	}

	if m.inCode {
		m.codeLines = append(m.codeLines, line)
		return
	}

	// Table rows: consecutive lines starting with | (header, alignment
	// separator, data). Flush the table when the run ends.
	if isTableRow(trimmed) {
		if !m.inTable {
			m.inTable = true
			m.tableRows = nil
		}
		m.tableRows = append(m.tableRows, splitTableRow(trimmed))
		return
	}
	if m.inTable {
		m.flushTable()
	}

	// Blockquote.
	if strings.HasPrefix(trimmed, ">") {
		m.out(renderBlockquote(trimmed))
		return
	}

	// Horizontal rule.
	if isHorizontalRule(trimmed) {
		m.out(ANSIReset + ANSIGray + strings.Repeat("─", 40) + ANSIReset + "\n")
		return
	}

	// Headings.
	if level := headingLevel(trimmed); level > 0 {
		m.out(renderHeading(trimmed[level:], level))
		return
	}

	// Lists: bullet or numbered.
	if isListItem(trimmed) {
		m.out(renderListItem(trimmed))
		return
	}

	// Plain paragraph (inline styles only).
	if trimmed == "" {
		m.out("\n")
		return
	}
	m.out(renderInline(trimmed) + "\n")
}

func (m *markdownStream) flushCode() {
	if !m.inCode {
		return
	}
	m.inCode = false
	lang := strings.TrimSpace(m.codeLang)
	border := ANSIGray + "┌─ " + lang + " " + strings.Repeat("─", maxInt(0, 46-len([]rune(lang)))) + ANSIReset
	m.out(border + "\n")
	for _, l := range m.codeLines {
		// Escape nothing — code is printed verbatim, just dimmed.
		m.out(ANSIGray + "│ " + l + ANSIReset + "\n")
	}
	m.out(ANSIGray + "└" + strings.Repeat("─", 48) + ANSIReset + "\n")
	m.codeLines = nil
}

func (m *markdownStream) flushTable() {
	if !m.inTable {
		return
	}
	m.inTable = false
	rows := m.tableRows
	m.tableRows = nil
	if len(rows) == 0 {
		return
	}

	// Column count from the widest row.
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return
	}

	// Alignments from the separator row (|---|, :---, ---:, :---:).
	align := make([]int, cols) // 0 left, 1 center, 2 right
	dataStart := 1
	if len(rows) > 1 && isAlignRow(rows[1]) {
		for i := 0; i < cols && i < len(rows[1]); i++ {
			c := rows[1][i]
			left := strings.HasPrefix(c, ":")
			right := strings.HasSuffix(c, ":")
			switch {
			case left && right:
				align[i] = 1
			case right:
				align[i] = 2
			}
		}
		dataStart = 2
	}

	// Column display widths (CJK-aware).
	widths := make([]int, cols)
	allRows := append([][]string{rows[0]}, rows[dataStart:]...)
	for _, r := range allRows {
		for i := 0; i < cols && i < len(r); i++ {
			if w := DisplayWidth(r[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	sep := func(left, mid, right string) string {
		var sb strings.Builder
		sb.WriteString(ANSIGray + left)
		for i := 0; i < cols; i++ {
			sb.WriteString(strings.Repeat("─", widths[i]+2))
			if i < cols-1 {
				sb.WriteString(mid)
			}
		}
		sb.WriteString(right + ANSIReset + "\n")
		return sb.String()
	}
	row := func(r []string, bold bool) string {
		var sb strings.Builder
		sb.WriteString(ANSIGray + "│" + ANSIReset)
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			w := widths[i]
			switch align[i] {
			case 1:
				// Center: pad left by half the remaining width, then pad to w.
				leftPad := (w - DisplayWidth(cell)) / 2
				cell = strings.Repeat(" ", leftPad) + PadDisplay(cell, w-leftPad)
			case 2:
				pad := w - DisplayWidth(cell)
				cell = strings.Repeat(" ", pad) + cell
			default:
				cell = PadDisplay(cell, w)
			}
			if bold {
				sb.WriteString(" " + ANSIBold + cell + ANSIReset + " ")
			} else {
				sb.WriteString(" " + cell + " ")
			}
			sb.WriteString(ANSIGray + "│" + ANSIReset)
		}
		sb.WriteString("\n")
		return sb.String()
	}

	m.out(sep("┌", "┬", "┐"))
	m.out(row(rows[0], true)) // header
	if dataStart > 1 {
		m.out(sep("├", "┼", "┤"))
	}
	for _, r := range allRows[1:] {
		m.out(row(r, false))
	}
	m.out(sep("└", "┴", "┘"))
}

// ─── block renderers ───────────────────────────────────────────

func renderHeading(text string, level int) string {
	repeat := level
	if repeat > 6 {
		repeat = 6
	}
	mark := strings.Repeat("#", repeat)
	// H1 gets a divider line under it.
	s := ANSICyan + ANSIBold + mark + " " + renderInline(strings.TrimSpace(text)) + ANSIReset + "\n"
	if level == 1 {
		s += ANSIGray + strings.Repeat("─", 40) + ANSIReset + "\n"
	}
	return s
}

func renderBlockquote(text string) string {
	body := strings.TrimSpace(strings.TrimPrefix(text, ">"))
	return ANSIGray + "│ " + renderInline(body) + ANSIReset + "\n"
}

func renderListItem(text string) string {
	trimmed := strings.TrimSpace(text)
	// Preserve nesting depth from leading spaces.
	depth := len(text) - len(strings.TrimLeft(text, " \t"))
	indent := strings.Repeat("  ", depth/2)

	var marker, body string
	if len(trimmed) > 0 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') {
		marker = ANSICyan + "•" + ANSIReset
		body = strings.TrimSpace(trimmed[1:])
	} else if len(trimmed) > 0 && trimmed[0] >= '0' && trimmed[0] <= '9' {
		// numbered list: keep the number
		j := 0
		for j < len(trimmed) && (trimmed[j] >= '0' && trimmed[j] <= '9' || trimmed[j] == '.') {
			j++
		}
		marker = ANSICyan + trimmed[:j] + ANSIReset
		body = strings.TrimSpace(trimmed[j:])
	} else {
		return renderInline(trimmed) + "\n"
	}
	return indent + marker + " " + renderInline(body) + "\n"
}

func isHorizontalRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	for _, r := range s {
		if r != '-' && r != '*' && r != '_' {
			return false
		}
	}
	return true
}

func headingLevel(s string) int {
	i := 0
	for i < len(s) && s[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(s) || s[i] != ' ' {
		return 0
	}
	return i
}

func isListItem(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if c == '-' || c == '*' || c == '+' {
		return len(s) == 1 || s[1] == ' '
	}
	if c >= '0' && c <= '9' {
		j := 0
		for j < len(s) && (s[j] >= '0' && s[j] <= '9') {
			j++
		}
		return j > 0 && j < len(s) && s[j] == '.'
	}
	return false
}

// ─── tables ────────────────────────────────────────────────────

func isTableRow(s string) bool {
	if !strings.HasPrefix(s, "|") {
		return false
	}
	return strings.Count(s, "|") >= 2
}

func isAlignRow(cells []string) bool {
	for _, c := range cells {
		t := strings.TrimSpace(strings.Trim(c, ":"))
		if t == "" || strings.Trim(t, "-") != "" {
			return false
		}
	}
	return true
}

func splitTableRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// ─── inline styles ─────────────────────────────────────────────

// renderInline applies inline markdown styles: `code`, **bold**, [text](url).
// Backticks are handled first so code spans are never styled further.
func renderInline(s string) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		// Inline code span.
		if s[i] == '`' {
			j := strings.IndexByte(s[i+1:], '`')
			if j >= 0 {
				sb.WriteString(ANSIYellow)
				sb.WriteString(s[i+1 : i+1+j])
				sb.WriteString(ANSIReset)
				i += j + 2
				continue
			}
		}
		// Bold **text**.
		if i+1 < len(s) && s[i] == '*' && s[i+1] == '*' {
			j := strings.Index(s[i+2:], "**")
			if j >= 0 {
				sb.WriteString(ANSIBold)
				sb.WriteString(s[i+2 : i+2+j])
				sb.WriteString(ANSIReset)
				i += j + 4
				continue
			}
		}
		// Link [text](url) — render text, drop the URL.
		if s[i] == '[' {
			j := strings.IndexByte(s[i+1:], ']')
			if j >= 0 && i+j+2 < len(s) && s[i+j+2] == '(' {
				k := strings.IndexByte(s[i+j+3:], ')')
				if k >= 0 {
					sb.WriteString(ANSIGray)
					sb.WriteString(s[i+1 : i+1+j])
					sb.WriteString(ANSIReset)
					i += j + k + 4
					continue
				}
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
