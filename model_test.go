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
