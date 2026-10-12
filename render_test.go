package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestEntryNameControlCharsAreNeutralised(t *testing.T) {
	applyTheme(themes[defaultTheme])
	e := entry{name: "evil\x1b]52;c;cHduZWQ=\a\x1b[2Jname"}
	for _, line := range []string{
		renderEntryLine(e, 60, false, false, false),
		renderEntryLine(e, 60, true, true, false),
		box(e.name, "", nil, 60, 1, true),
	} {
		for _, bad := range []string{"\x1b]52", "\a", "\x1b[2J"} {
			if strings.Contains(line, bad) {
				t.Fatalf("%q leaked into %q", bad, line)
			}
		}
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "evil?]52;c;cHduZWQ=??[2Jname") {
			t.Fatalf("name not rendered with replacements: %q", plain)
		}
	}
}
