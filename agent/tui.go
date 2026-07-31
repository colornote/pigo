package agent

import "strings"

// runeWidth returns the terminal display width of a rune:
// 2 for CJK / fullwidth / emoji, 1 otherwise, 0 for control chars.
func runeWidth(r rune) int {
	if r < 32 {
		return 0
	}
	if r < 0x7f {
		return 1
	}
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E,   // CJK Radicals / Kangxi / Symbols
		r >= 0x3041 && r <= 0x33FF,   // Hiragana / Katakana / CJK Compat
		r >= 0x3400 && r <= 0x4DBF,   // CJK Ext A
		r >= 0x4E00 && r <= 0x9FFF,   // CJK Unified
		r >= 0xA000 && r <= 0xA4CF,   // Yi
		r >= 0xAC00 && r <= 0xD7A3,   // Hangul Syllables
		r >= 0xF900 && r <= 0xFAFF,   // CJK Compat Ideographs
		r >= 0xFE30 && r <= 0xFE4F,   // CJK Compat Forms
		r >= 0xFF00 && r <= 0xFF60,   // Fullwidth Forms
		r >= 0xFFE0 && r <= 0xFFE6,   // Fullwidth signs
		r >= 0x1F300 && r <= 0x1FAFF, // Emoji
		r >= 0x20000 && r <= 0x2FFFD: // CJK Ext B+
		return 2
	}
	return 1
}

// DisplayWidth returns the terminal display width of s (CJK chars count as 2).
func DisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// PadDisplay right-pads s with spaces so its display width is at least n.
// Unlike fmt's %-Ns (which pads by rune count), this is CJK-aware.
func PadDisplay(s string, n int) string {
	w := DisplayWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// TruncateToWidth truncates s so its display width ≤ max, appending "…" if cut.
// Never splits a rune or leaves a dangling ANSI sequence (caller wraps colors).
func TruncateToWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if DisplayWidth(s) <= max {
		return s
	}
	var sb strings.Builder
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > max-1 {
			break
		}
		sb.WriteRune(r)
		w += rw
	}
	return sb.String() + "…"
}

// colorSeg is a (text, color) pair used to build width-aware colored lines.
type colorSeg struct {
	text  string
	color string // ANSI escape code; empty for plain text
}

// renderSegs joins colored segments, truncating the whole line to maxWidth
// display columns (0 = no truncation). Colors never break across the cut.
func renderSegs(segs []colorSeg, maxWidth int) string {
	var sb strings.Builder
	used := 0
	for _, s := range segs {
		sw := DisplayWidth(s.text)
		if maxWidth > 0 && used+sw > maxWidth {
			// TruncateToWidth appends "…" itself when cutting.
			remaining := maxWidth - used
			if remaining >= 1 {
				if tr := TruncateToWidth(s.text, remaining); tr != "" {
					if s.color != "" {
						sb.WriteString(s.color + tr + ANSIReset)
					} else {
						sb.WriteString(tr)
					}
				}
			}
			break
		}
		if s.color != "" {
			sb.WriteString(s.color)
		}
		sb.WriteString(s.text)
		if s.color != "" {
			sb.WriteString(ANSIReset)
		}
		used += sw
	}
	return sb.String()
}
