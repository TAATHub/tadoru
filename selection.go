package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// selPoint addresses a cell in preview display lines, independent of scroll.
type selPoint struct{ line, col int }

type selection struct {
	active, dragging bool
	anchor, head     selPoint
}

func (s selection) bounds() (selPoint, selPoint) {
	a, b := s.anchor, s.head
	if b.line < a.line || (b.line == a.line && b.col < a.col) {
		a, b = b, a
	}
	return a, b
}

func (s selection) empty() bool {
	return s.anchor == s.head
}

func selectable(p *previewContent) bool {
	return p != nil && (p.kind == pvText || p.kind == pvMarkdown || p.kind == pvBinary)
}

// colsFor returns the selected display columns [c0, c1) of a line, with
// c1 = -1 meaning "to the end of the line".
func (s selection) colsFor(line int) (int, int, bool) {
	a, b := s.bounds()
	if !s.active || line < a.line || line > b.line {
		return 0, 0, false
	}
	c0, c1 := 0, -1
	if line == a.line {
		c0 = a.col
	}
	if line == b.line {
		c1 = b.col + 1
	}
	return c0, c1, true
}

func highlightSelection(line string, c0, c1 int) string {
	w := ansi.StringWidth(line)
	if c1 < 0 || c1 > w {
		c1 = w
	}
	if c0 >= c1 {
		return line
	}
	st := lipgloss.NewStyle().Background(colSelBgDim).Foreground(colFg)
	return ansi.Truncate(line, c0, "") + "\x1b[0m" +
		st.Render(ansi.Strip(ansi.Cut(line, c0, c1))) + ansi.TruncateLeft(line, c1, "")
}

func textGutter(p *previewContent) int {
	return len(strconv.Itoa(len(p.text))) + 1
}

// selectedText reads plain text lines from the source instead of the display
// so that line numbers are skipped and truncated long lines are copied whole.
func (m *Model) selectedText(display []string, w int) string {
	if !m.sel.active || m.sel.empty() || m.preview == nil {
		return ""
	}
	a, b := m.sel.bounds()
	gutter := 0
	source := func(i int) string { return ansi.Strip(display[i]) }
	if m.preview.kind == pvText {
		gutter = textGutter(m.preview)
		source = func(i int) string { return ansi.Strip(m.preview.text[i]) }
	}
	var out []string
	for i := a.line; i <= b.line && i < len(display); i++ {
		c0, c1, _ := m.sel.colsFor(i)
		c0 = max(0, c0-gutter)
		// Reaching the right edge means the rest of a truncated line too.
		if c1 >= w {
			c1 = -1
		}
		if c1 >= 0 {
			c1 = max(0, c1-gutter)
		}
		text := source(i)
		if c1 < 0 {
			c1 = ansi.StringWidth(text)
		}
		out = append(out, strings.TrimRight(ansi.Cut(text, c0, c1), " "))
	}
	return strings.Join(out, "\n")
}

func copyToClipboard(text string) error {
	if path, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.Command(path)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	_, err := fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'()\[\]{}]+`)

// linkAt returns the URL under display column col of a rendered line.
func linkAt(line string, col int) string {
	plain := ansi.Strip(line)
	for _, loc := range urlPattern.FindAllStringIndex(plain, -1) {
		url := strings.TrimRight(plain[loc[0]:loc[1]], ".,;:!?")
		start := ansi.StringWidth(plain[:loc[0]])
		if col >= start && col < start+ansi.StringWidth(url) {
			return url
		}
	}
	return ""
}

func (m *Model) finishSelection() {
	m.sel.dragging = false
	if m.sel.empty() {
		m.sel.active = false
		// A click without dragging follows a link instead of selecting.
		lines := m.previewLines()
		if p := m.sel.anchor; p.line < len(lines) {
			if url := linkAt(lines[p.line], p.col); url != "" {
				if err := openDefault(url); err != nil {
					m.status = "open: " + err.Error()
				} else {
					m.status = "Opened " + url
				}
			}
		}
		return
	}
	text := m.selectedText(m.previewLines(), m.layout().vw)
	if text == "" {
		return
	}
	if err := copyToClipboard(text); err != nil {
		m.status = "copy: " + err.Error()
		return
	}
	m.status = fmt.Sprintf("Copied %d chars", utf8.RuneCountInString(text))
}
