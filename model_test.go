package main

import (
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
