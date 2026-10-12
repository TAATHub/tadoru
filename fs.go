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

func readEntries(dir string) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(des))
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
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
