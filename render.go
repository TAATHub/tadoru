package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Set by applyTheme before the program starts.
var (
	colAccent, colBorder, colDim, colFg lipgloss.Color
	colSelBg, colSelBgDim, colSelFg     lipgloss.Color

	dirStyle, linkStyle, execStyle, fileStyle lipgloss.Style
	dimStyle, gutterStyle, errStyle           lipgloss.Style
	headerStyle, pathStyle                    lipgloss.Style
)

// box draws a rounded frame with inner size w×h, a title embedded in the top
// border and a footer embedded right-aligned in the bottom border.
func box(title, footer string, body []string, w, h int, active bool) string {
	bc := colBorder
	if active {
		bc = colAccent
	}
	bs := lipgloss.NewStyle().Foreground(bc)
	ts := lipgloss.NewStyle().Foreground(bc).Bold(true)
	if !active {
		ts = ts.Foreground(colFg)
	}

	var sb strings.Builder
	sb.WriteString(bs.Render("╭"))
	used := 0
	if title != "" && w > 4 {
		t := ansi.Truncate(title, w-3, "…")
		sb.WriteString(bs.Render("─ ") + ts.Render(t) + bs.Render(" "))
		used = ansi.StringWidth(t) + 3
	}
	sb.WriteString(bs.Render(strings.Repeat("─", max(0, w-used)) + "╮"))
	sb.WriteByte('\n')

	side := bs.Render("│")
	for i := 0; i < h; i++ {
		line := ""
		if i < len(body) {
			line = ansi.Truncate(body[i], w, "")
		}
		sb.WriteString(side + line + "\x1b[0m" + strings.Repeat(" ", max(0, w-ansi.StringWidth(line))) + side + "\n")
	}

	f, fw := "", 0
	if footer != "" && w > 4 {
		f = ansi.Truncate(footer, w-3, "…")
		fw = ansi.StringWidth(f) + 3
	}
	sb.WriteString(bs.Render("╰" + strings.Repeat("─", max(0, w-fw))))
	if f != "" {
		sb.WriteString(bs.Render(" ") + dimStyle.Render(f) + bs.Render(" ─"))
	}
	sb.WriteString(bs.Render("╯"))
	return sb.String()
}

func entryStyle(e entry) (lipgloss.Style, string) {
	switch {
	case e.isDir:
		return dirStyle, e.name + "/"
	case e.isLink:
		return linkStyle, e.name
	case e.isExec():
		return execStyle, e.name
	}
	return fileStyle, e.name
}

func renderEntryLine(e entry, w int, selected, active, showSize bool) string {
	style, name := entryStyle(e)
	right := ""
	if showSize && !e.isDir {
		right = humanSize(e.size)
	}
	avail := w - 2
	if right != "" {
		avail -= ansi.StringWidth(right) + 1
	}
	if avail < 4 {
		right, avail = "", w-2
	}
	name = ansi.Truncate(name, max(1, avail), "…")
	gap := max(0, w-2-ansi.StringWidth(name)-ansi.StringWidth(right))

	if selected {
		bg := colSelBgDim
		fg := colFg
		if active {
			bg, fg = colSelBg, colSelFg
		}
		return lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true).
			Render(" " + name + strings.Repeat(" ", gap) + right + " ")
	}
	return " " + style.Render(name) + strings.Repeat(" ", gap) + dimStyle.Render(right) + " "
}

func renderList(entries []entry, cursor, offset, w, h int, active, showSize bool) []string {
	lines := make([]string, 0, h)
	for i := offset; i < len(entries) && i < offset+h; i++ {
		lines = append(lines, renderEntryLine(entries[i], w, i == cursor, active, showSize))
	}
	return lines
}
