package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKittyKeyChangesWithFileVersion(t *testing.T) {
	p := &previewContent{path: "/x.png", modTime: time.Unix(1, 0), size: 10}
	before := kittyKey(p, 80, 40)
	p.modTime = time.Unix(2, 0)
	if kittyKey(p, 80, 40) == before {
		t.Fatal("kitty key must change when the file is modified")
	}
}

func TestVimKeysMoveCursor(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(dir)
	m.width, m.height = 120, 40
	m.handleKey("j")
	if m.cursor != 1 {
		t.Fatalf("j: cursor = %d, want 1", m.cursor)
	}
	m.handleKey("k")
	m.handleKey("l")
	if m.cwd != filepath.Join(dir, "a") {
		t.Fatalf("l: cwd = %s", m.cwd)
	}
	m.handleKey("h")
	if m.cwd != dir {
		t.Fatalf("h: cwd = %s", m.cwd)
	}
}

func TestToggleHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".hidden", "visible"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(dir)
	if len(m.entries) != 1 {
		t.Fatalf("default: %d entries, want 1", len(m.entries))
	}
	m.handleKey(".")
	if len(m.entries) != 2 {
		t.Fatalf("shown: %d entries, want 2", len(m.entries))
	}
	m.handleKey(".")
	if len(m.entries) != 1 {
		t.Fatalf("hidden again: %d entries, want 1", len(m.entries))
	}
}

func TestSearchFindsNestedEntries(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(dir, "target.txt"), filepath.Join(nested, "target.go"), filepath.Join(dir, "other")} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := newModel(dir)
	m.width, m.height = 120, 40
	m.setFilter("target")
	for msg := range m.treeCh {
		m.Update(msg)
	}
	if m.treeWalking {
		t.Fatal("walk did not finish")
	}
	var names []string
	for _, e := range m.entries {
		names = append(names, e.name)
	}
	want := []string{"target.txt", filepath.Join("a", "b", "target.go")}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("matches = %v, want %v", names, want)
	}
	m.moveTo(1)
	m.enter()
	if m.cwd != nested || m.filter != "" {
		t.Fatalf("enter: cwd = %s filter = %q", m.cwd, m.filter)
	}
	if sel := m.selected(); sel == nil || sel.name != "target.go" {
		t.Fatalf("enter: selected = %v", sel)
	}
}
