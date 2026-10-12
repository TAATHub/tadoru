package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type imageMode int

const (
	imageBlocks imageMode = iota
	imageKitty
)

func detectImageMode() imageMode {
	switch os.Getenv("TADORU_IMAGE") {
	case "kitty":
		return imageKitty
	case "blocks":
		return imageBlocks
	}
	// tmux drops Kitty graphics without explicit passthrough setup.
	if os.Getenv("TMUX") != "" {
		return imageBlocks
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" ||
		strings.Contains(os.Getenv("TERM"), "kitty") || strings.Contains(os.Getenv("TERM"), "ghostty") ||
		os.Getenv("TERM_PROGRAM") == "WezTerm" {
		return imageKitty
	}
	return imageBlocks
}

// queriedCell holds the answer to CSI 16 t for terminals (or multiplexers)
// that leave the pixel fields of TIOCGWINSZ at zero.
var queriedCellW, queriedCellH float64

// cellPixelSize falls back to a Retina-sized cell when nothing reports pixel
// dimensions: overestimating only makes the terminal downscale a sharper
// image, while underestimating makes it upscale a blurry one.
func cellPixelSize() (float64, float64) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err == nil && ws.Xpixel > 0 && ws.Ypixel > 0 && ws.Col > 0 && ws.Row > 0 {
		return float64(ws.Xpixel) / float64(ws.Col), float64(ws.Ypixel) / float64(ws.Row)
	}
	if queriedCellW > 0 && queriedCellH > 0 {
		return queriedCellW, queriedCellH
	}
	return 20, 40
}

var cellSizeReply = regexp.MustCompile(`\x1b\[6;(\d+);(\d+)t`)

// queryCellSize must run before Bubble Tea starts reading stdin. It polls
// instead of blocking on Read so that no reader is left behind to steal
// input when the terminal never answers.
func queryCellSize() {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return
	}
	defer term.Restore(fd, old)
	if _, err := os.Stdout.WriteString("\x1b[16t"); err != nil {
		return
	}
	var resp []byte
	buf := make([]byte, 64)
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(left.Milliseconds())+1)
		if err != nil || n == 0 {
			return
		}
		n, err = unix.Read(fd, buf)
		if err != nil || n <= 0 {
			return
		}
		resp = append(resp, buf[:n]...)
		if m := cellSizeReply.FindSubmatch(resp); m != nil {
			h, _ := strconv.Atoi(string(m[1]))
			w, _ := strconv.Atoi(string(m[2]))
			if w > 0 && h > 0 {
				queriedCellW, queriedCellH = float64(w), float64(h)
			}
			return
		}
	}
}

type kittyImage struct {
	key        string
	id         uint32
	cols, rows int
	payload    string
}

type kittyReadyMsg struct{ img *kittyImage }

func kittyKey(path string, w, h int) string {
	return fmt.Sprintf("%s|%d|%d", path, w, h)
}

// encodeKitty fits img into a w×h cell box and returns the PNG transmission
// for a virtual placement of cols×rows cells.
func encodeKitty(img image.Image, key string, id uint32, w, h int, cw, ch float64) *kittyImage {
	b := img.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	scale := math.Min(float64(w)*cw/iw, float64(h)*ch/ih)
	cols := clamp(int(math.Ceil(iw*scale/cw)), 1, w)
	rows := clamp(int(math.Ceil(ih*scale/ch)), 1, h)

	src := img
	if scale < 1 {
		src = scaleToFit(img, max(1, int(iw*scale)), max(1, int(ih*scale)))
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, src); err != nil {
		return nil
	}
	data := base64.StdEncoding.EncodeToString(buf.Bytes())

	var sb strings.Builder
	const chunk = 4096
	for i := 0; i < len(data); i += chunk {
		end := min(len(data), i+chunk)
		more := 0
		if end < len(data) {
			more = 1
		}
		if i == 0 {
			fmt.Fprintf(&sb, "\x1b_Ga=T,U=1,f=100,t=d,q=2,i=%d,c=%d,r=%d,m=%d;%s\x1b\\", id, cols, rows, more, data[i:end])
		} else {
			fmt.Fprintf(&sb, "\x1b_Gm=%d;%s\x1b\\", more, data[i:end])
		}
	}
	return &kittyImage{key: key, id: id, cols: cols, rows: rows, payload: sb.String()}
}

func kittyDelete(id uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

// placeholderLines renders the Unicode placeholder grid; the foreground
// colour carries the image id and the diacritics carry row and column.
func (k *kittyImage) placeholderLines(w int) []string {
	if k.rows > len(kittyDiacritics) || k.cols > len(kittyDiacritics) {
		return nil
	}
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (k.id>>16)&0xff, (k.id>>8)&0xff, k.id&0xff)
	pad := strings.Repeat(" ", max(0, (w-k.cols)/2))
	lines := make([]string, k.rows)
	for r := 0; r < k.rows; r++ {
		var sb strings.Builder
		sb.WriteString(pad + fg)
		for c := 0; c < k.cols; c++ {
			sb.WriteRune('\U0010EEEE')
			sb.WriteRune(kittyDiacritics[r])
			sb.WriteRune(kittyDiacritics[c])
		}
		sb.WriteString("\x1b[39m")
		lines[r] = sb.String()
	}
	return lines
}
