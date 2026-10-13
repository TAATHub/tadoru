package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
}

func TestExtractBlocks(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "a b.png"), 40, 20)
	src := strings.Join([]string{
		"![local](a%20b.png)",
		"<p align=\"center\"><img src=\"a b.png\" width=\"300\"></p>",
		"![remote](https://example.com/a.png)",
		"![missing](nope.png)",
		"```md",
		"![in fence](a%20b.png)",
		"```",
		"```mermaid",
		"graph LR",
		"  A --> B",
		"```",
	}, "\n")
	text, blocks := extractBlocks(filepath.Join(dir, "doc.md"), src)
	var kinds []mdBlockKind
	for _, b := range blocks {
		kinds = append(kinds, b.kind)
	}
	if want := []mdBlockKind{mdImage, mdImage, mdMermaid}; len(kinds) != len(want) || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("blocks = %v, want %v", kinds, want)
	}
	for _, keep := range []string{"![remote]", "![missing]", "![in fence]"} {
		if !strings.Contains(text, keep) {
			t.Errorf("%q should be left to glamour", keep)
		}
	}
	if strings.Contains(text, "graph LR") {
		t.Error("mermaid source should be replaced by a token")
	}
}

func TestMarkdownKittyPlaceholders(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "a.png"), 200, 100)
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(doc, []byte("# Title\n\n![a](a.png)\n\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newModel(dir)
	m.width, m.height = 200, 50
	m.imgMode, m.cellW, m.cellH = imageKitty, 10, 20
	m.preview = buildPreview(doc)
	m.ensureKitty()
	if len(m.plan) != 1 {
		t.Fatalf("plan has %d images, want 1", len(m.plan))
	}
	k := m.plan[0]
	// 200×100 px at 10×20 px cells, never upscaled.
	if k.cols != 20 || k.rows != 5 {
		t.Fatalf("box = %d×%d, want 20×5", k.cols, k.rows)
	}
	rows := 0
	var after bool
	for _, l := range m.previewLines() {
		if strings.ContainsRune(l, '\U0010EEEE') {
			rows++
		}
		after = after || strings.Contains(ansi.Strip(l), "after")
	}
	if rows != k.rows || !after {
		t.Fatalf("placeholder rows = %d (want %d), text after image shown = %v", rows, k.rows, after)
	}
}

func TestMarkdownSourceToggle(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(doc, []byte("# Title\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newModel(dir)
	m.width, m.height = 120, 40
	m.preview = buildPreview(doc)
	m.handleKey("r")
	if got := ansi.Strip(m.previewLines()[0]); got != "1 # Title" {
		t.Fatalf("source view first line = %q", got)
	}
	m.handleKey("r")
	if got := strings.TrimSpace(ansi.Strip(m.previewLines()[0])); got != "Title" {
		t.Fatalf("rendered view first line = %q", got)
	}
}
