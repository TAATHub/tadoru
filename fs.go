package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const walkBatchInterval = 50 * time.Millisecond

type entry struct {
	name    string
	path    string
	isDir   bool
	isLink  bool
	mode    fs.FileMode
	size    int64
	modTime time.Time
}

func (e entry) isExec() bool {
	return !e.isDir && e.mode&0o111 != 0
}

func readEntries(dir string, showHidden bool) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(des))
	for _, de := range des {
		name := de.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		e := entry{
			name:   name,
			path:   filepath.Join(dir, name),
			isDir:  de.IsDir(),
			isLink: de.Type()&fs.ModeSymlink != 0,
		}
		if info, err := de.Info(); err == nil {
			e.mode, e.size, e.modTime = info.Mode(), info.Size(), info.ModTime()
		}
		if e.isLink {
			if st, err := os.Stat(e.path); err == nil {
				e.isDir = st.IsDir()
				e.size = st.Size()
			}
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.isDir != b.isDir {
			return a.isDir
		}
		return naturalLess(strings.ToLower(a.name), strings.ToLower(b.name))
	})
	return entries, nil
}

// naturalLess orders "file2" before "file10".
func naturalLess(a, b string) bool {
	for len(a) > 0 && len(b) > 0 {
		if isDigit(a[0]) && isDigit(b[0]) {
			na, nb := digitPrefixLen(a), digitPrefixLen(b)
			ta, tb := strings.TrimLeft(a[:na], "0"), strings.TrimLeft(b[:nb], "0")
			if len(ta) != len(tb) {
				return len(ta) < len(tb)
			}
			if ta != tb {
				return ta < tb
			}
			a, b = a[na:], b[nb:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digitPrefixLen(s string) int {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func abbrevHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

type treeBatchMsg struct {
	gen       int
	entries   []entry
	done      bool
	truncated bool
}

// walkEntries lists everything under root breadth-first, so shallower matches
// come first, naming each entry by its path relative to root, and sends them
// to ch in batches until it finishes, reaches limit or stop is closed.
// Symlinked directories are not descended into to avoid cycles.
func walkEntries(root string, showHidden bool, limit, gen int, ch chan<- treeBatchMsg, stop <-chan struct{}) {
	defer close(ch)
	var batch []entry
	total := 0
	last := time.Now()
	send := func(done, truncated bool) bool {
		select {
		case ch <- treeBatchMsg{gen: gen, entries: batch, done: done, truncated: truncated}:
			batch, last = nil, time.Now()
			return true
		case <-stop:
			return false
		}
	}
	queue := []string{root}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		entries, err := readEntries(dir, showHidden)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if total >= limit {
				send(true, true)
				return
			}
			if rel, err := filepath.Rel(root, e.path); err == nil {
				e.name = rel
			}
			batch = append(batch, e)
			total++
			if e.isDir && !e.isLink {
				queue = append(queue, e.path)
			}
		}
		if len(batch) > 0 && time.Since(last) >= walkBatchInterval {
			if !send(false, false) {
				return
			}
		}
	}
	send(true, false)
}
