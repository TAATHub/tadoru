package main

import "testing"

func TestLinkAt(t *testing.T) {
	line := "  See \x1b[4mhttps://example.com/docs?a=1\x1b[0m and 日本語 https://bar.io/x."
	tests := []struct {
		col  int
		want string
	}{
		{5, ""},
		{6, "https://example.com/docs?a=1"},
		{33, "https://example.com/docs?a=1"},
		{34, ""},
		{48, "https://bar.io/x"},
		{64, ""},
	}
	for _, tt := range tests {
		if got := linkAt(line, tt.col); got != tt.want {
			t.Errorf("linkAt(col=%d) = %q, want %q", tt.col, got, tt.want)
		}
	}
}
