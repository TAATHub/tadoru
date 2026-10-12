package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	themeFlag := flag.String("theme", "", "color theme ("+strings.Join(themeNames(), ", ")+")")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: tadoru [-theme NAME] [dir]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	name := *themeFlag
	if name == "" {
		name = os.Getenv("TADORU_THEME")
	}
	if name != "" {
		t, ok := lookupTheme(name)
		if !ok {
			fmt.Fprintf(os.Stderr, "tadoru: unknown theme %q (available: %s)\n", name, strings.Join(themeNames(), ", "))
			os.Exit(2)
		}
		applyTheme(t)
	} else if t, ok := lookupTheme(ghosttyTheme()); ok {
		applyTheme(t)
	} else {
		applyTheme(themes[defaultTheme])
	}

	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		var st os.FileInfo
		if st, err = os.Stat(abs); err == nil && !st.IsDir() {
			err = fmt.Errorf("%s is not a directory", dir)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tadoru:", err)
		os.Exit(1)
	}

	if detectImageMode() == imageKitty {
		queryCellSize()
	}
	m := newModel(abs)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	m.clearKitty()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tadoru:", err)
		os.Exit(1)
	}
}
