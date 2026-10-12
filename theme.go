package main

import (
	"bufio"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

type theme struct {
	bg, fg, accent, border, dim, gutter string
	dir, link, exec, err                string
	chroma                              string
	glamour                             *ansi.StyleConfig
}

const defaultTheme = "catppuccin-mocha"

var themes = map[string]theme{
	"dracula": {
		bg: "#282a36", fg: "#f8f8f2", accent: "#bd93f9", border: "#44475a", dim: "#6272a4", gutter: "#6272a4",
		dir: "#bd93f9", link: "#8be9fd", exec: "#50fa7b", err: "#ff5555",
		chroma: "dracula", glamour: &glamourstyles.DraculaStyleConfig,
	},
	"catppuccin-mocha": {
		bg: "#1e1e2e", fg: "#cdd6f4", accent: "#89b4fa", border: "#45475a", dim: "#7f849c", gutter: "#585b70",
		dir: "#89b4fa", link: "#94e2d5", exec: "#a6e3a1", err: "#f38ba8",
		chroma: "catppuccin-mocha",
	},
	"tokyonight": {
		bg: "#1a1b26", fg: "#c0caf5", accent: "#7aa2f7", border: "#3b4261", dim: "#565f89", gutter: "#3b4261",
		dir: "#7aa2f7", link: "#7dcfff", exec: "#9ece6a", err: "#f7768e",
		chroma: "tokyonight-night", glamour: &glamourstyles.TokyoNightStyleConfig,
	},
	"nord": {
		bg: "#2e3440", fg: "#d8dee9", accent: "#88c0d0", border: "#4c566a", dim: "#616e88", gutter: "#4c566a",
		dir: "#81a1c1", link: "#8fbcbb", exec: "#a3be8c", err: "#bf616a",
		chroma: "nord",
	},
	"gruvbox": {
		bg: "#282828", fg: "#ebdbb2", accent: "#83a598", border: "#504945", dim: "#928374", gutter: "#665c54",
		dir: "#83a598", link: "#8ec07c", exec: "#b8bb26", err: "#fb4934",
		chroma: "gruvbox",
	},
	"solarized-dark": {
		bg: "#002b36", fg: "#93a1a1", accent: "#268bd2", border: "#073642", dim: "#586e75", gutter: "#586e75",
		dir: "#268bd2", link: "#2aa198", exec: "#859900", err: "#dc322f",
		chroma: "solarized-dark",
	},
	"monokai": {
		bg: "#272822", fg: "#f8f8f2", accent: "#66d9ef", border: "#49483e", dim: "#75715e", gutter: "#75715e",
		dir: "#66d9ef", link: "#ae81ff", exec: "#a6e22e", err: "#f92672",
		chroma: "monokai",
	},
	"onedark": {
		bg: "#282c34", fg: "#abb2bf", accent: "#61afef", border: "#3e4451", dim: "#5c6370", gutter: "#4b5263",
		dir: "#61afef", link: "#56b6c2", exec: "#98c379", err: "#e06c75",
		chroma: "onedark",
	},
}

var themeAliases = map[string]string{
	"catppuccin":       "catppuccin-mocha",
	"tokyo-night":      "tokyonight",
	"tokyonight-night": "tokyonight",
	"tokyonight-storm": "tokyonight",
	"gruvbox-dark":     "gruvbox",
	"one-dark":         "onedark",
	"atom-one-dark":    "onedark",
	"monokai-pro":      "monokai",
}

func themeNames() []string {
	names := make([]string, 0, len(themes))
	for n := range themes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// lookupTheme accepts terminal-style names such as "Catppuccin Mocha" or
// "Dracula+" by normalising case and separators.
func lookupTheme(name string) (theme, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.NewReplacer(" ", "-", "_", "-", "+", "").Replace(n)
	if a, ok := themeAliases[n]; ok {
		n = a
	}
	t, ok := themes[n]
	return t, ok
}

// ghosttyTheme returns the dark variant when the config uses
// "theme = light:X,dark:Y".
func ghosttyTheme() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	paths := []string{
		filepath.Join(home, ".config", "ghostty", "config"),
		filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty", "config"),
	}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		var value string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), "=")
			if ok && strings.TrimSpace(k) == "theme" {
				value = strings.Trim(strings.TrimSpace(v), `"`)
			}
		}
		f.Close()
		if value == "" {
			continue
		}
		for _, part := range strings.Split(value, ",") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(part), "dark:"); ok {
				return v
			}
		}
		if !strings.Contains(value, ":") {
			return value
		}
	}
	return ""
}

var current theme

func applyTheme(t theme) {
	current = t
	c := func(s string) lipgloss.Color { return lipgloss.Color(s) }
	colAccent, colBorder, colDim = c(t.accent), c(t.border), c(t.dim)
	colSelBg, colSelBgDim, colSelFg, colFg = c(t.accent), c(t.border), c(t.bg), c(t.fg)

	dirStyle = lipgloss.NewStyle().Foreground(c(t.dir)).Bold(true)
	linkStyle = lipgloss.NewStyle().Foreground(c(t.link)).Italic(true)
	execStyle = lipgloss.NewStyle().Foreground(c(t.exec))
	fileStyle = lipgloss.NewStyle().Foreground(colFg)
	dimStyle = lipgloss.NewStyle().Foreground(colDim)
	gutterStyle = lipgloss.NewStyle().Foreground(c(t.gutter))
	errStyle = lipgloss.NewStyle().Foreground(c(t.err))
	headerStyle = lipgloss.NewStyle().Foreground(colSelFg).Background(colAccent).Bold(true).Padding(0, 1)
	pathStyle = lipgloss.NewStyle().Foreground(colFg).Bold(true)

	imageBg = hexColor(t.bg)
}

func glamourStyle() ansi.StyleConfig {
	if current.glamour != nil {
		return *current.glamour
	}
	cfg := glamourstyles.DarkStyleConfig
	cfg.CodeBlock.Chroma = nil
	cfg.CodeBlock.Theme = current.chroma
	return cfg
}

func hexColor(s string) color.RGBA {
	var r, g, b uint8
	if len(s) == 7 && s[0] == '#' {
		parse := func(h string) uint8 {
			var v uint8
			for _, ch := range h {
				v <<= 4
				switch {
				case ch >= '0' && ch <= '9':
					v |= uint8(ch - '0')
				case ch >= 'a' && ch <= 'f':
					v |= uint8(ch-'a') + 10
				case ch >= 'A' && ch <= 'F':
					v |= uint8(ch-'A') + 10
				}
			}
			return v
		}
		r, g, b = parse(s[1:3]), parse(s[3:5]), parse(s[5:7])
	}
	return color.RGBA{r, g, b, 0xff}
}
