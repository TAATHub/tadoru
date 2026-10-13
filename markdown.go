package main

import (
	"fmt"
	"image"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/AlexanderGrooff/mermaid-ascii/pkg/diagram"
	mermaid "github.com/AlexanderGrooff/mermaid-ascii/pkg/render"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Each embedded image stays decoded in the preview cache.
const maxMarkdownImages = 12

type mdBlockKind int

const (
	mdImage mdBlockKind = iota
	mdMermaid
)

type mdBlock struct {
	kind mdBlockKind
	img  image.Image
	code string
}

var (
	mdImageLine  = regexp.MustCompile(`^\s*!\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+["'][^"']*["'])?\s*\)\s*$`)
	htmlImageTag = regexp.MustCompile(`^\s*(?:<[^>]+>\s*)*<img\s[^>]*\bsrc\s*=\s*["']([^"']+)["'][^>]*>(?:\s*<[^>]+>)*\s*$`)
	fenceOpen    = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})\\s*([^\\s`]*)")
	blockToken   = regexp.MustCompile(`^\s*TADORUBLOCK(\d{4})\s*$`)
)

// extractBlocks swaps standalone local images and mermaid fences for
// paragraph tokens, so glamour lays out the rest of the document in one pass
// and the tokens' rendered lines can be replaced afterwards.
func extractBlocks(mdPath, src string) (string, []mdBlock) {
	var blocks []mdBlock
	var out []string
	token := func(indent string) {
		out = append(out, "", indent+fmt.Sprintf("TADORUBLOCK%04d", len(blocks)-1), "")
	}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if m := fenceOpen.FindStringSubmatch(line); m != nil {
			end := i + 1
			for end < len(lines) && !isFenceClose(lines[end], m[1]) {
				end++
			}
			if strings.EqualFold(m[2], "mermaid") {
				blocks = append(blocks, mdBlock{kind: mdMermaid, code: strings.Join(lines[i+1:min(end, len(lines))], "\n")})
				token("")
			} else {
				out = append(out, lines[i:min(end+1, len(lines))]...)
			}
			i = end
			continue
		}
		if countImages(blocks) < maxMarkdownImages {
			if img := loadMarkdownImage(mdPath, line); img != nil {
				blocks = append(blocks, mdBlock{kind: mdImage, img: img})
				token(line[:len(line)-len(strings.TrimLeft(line, " \t"))])
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), blocks
}

func isFenceClose(line, fence string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, fence[:3]) && strings.Trim(t, fence[:1]) == "" && len(t) >= len(fence)
}

func countImages(blocks []mdBlock) int {
	n := 0
	for _, b := range blocks {
		if b.kind == mdImage {
			n++
		}
	}
	return n
}

// loadMarkdownImage decodes the image a line consists of. Remote images are
// left to glamour's alt text: previewing a file must not touch the network.
func loadMarkdownImage(mdPath, line string) image.Image {
	m := mdImageLine.FindStringSubmatch(line)
	if m == nil {
		m = htmlImageTag.FindStringSubmatch(line)
	}
	if m == nil {
		return nil
	}
	src := m[1]
	if strings.Contains(src, "://") || strings.HasPrefix(src, "data:") {
		return nil
	}
	src, _, _ = strings.Cut(src, "#")
	src, _, _ = strings.Cut(src, "?")
	if u, err := url.PathUnescape(src); err == nil {
		src = u
	}
	if rest, ok := strings.CutPrefix(src, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		src = filepath.Join(home, rest)
	} else if !filepath.IsAbs(src) {
		src = filepath.Join(filepath.Dir(mdPath), src)
	}
	if !isImageExt(src) {
		return nil
	}
	img, _, _, err := decodeImage(src)
	if err != nil {
		return nil
	}
	return img
}

// imageBoxFunc lays out embedded image i inside at most w×h cells.
type imageBoxFunc func(i int, img image.Image, w, h int) []string

// renderMarkdownDoc renders the text with glamour and splices each block's
// lines in place of its token. A nil imgFn drops images, leaving no gap.
func renderMarkdownDoc(src string, blocks []mdBlock, w, h int, imgFn imageBoxFunc) ([]string, error) {
	lines, err := renderMarkdown(src, w)
	if err != nil || len(blocks) == 0 {
		return lines, err
	}
	const indent = "  "
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		m := blockToken.FindStringSubmatch(ansi.Strip(line))
		if m == nil {
			out = append(out, line)
			continue
		}
		i, _ := strconv.Atoi(m[1])
		if i >= len(blocks) {
			continue
		}
		b := blocks[i]
		switch b.kind {
		case mdImage:
			if imgFn != nil {
				bw, bh := mdImageBox(w, h)
				for _, l := range imgFn(i, b.img, bw, bh) {
					out = append(out, indent+l)
				}
			}
		case mdMermaid:
			for _, l := range renderMermaid(b.code, w-2*len(indent)) {
				out = append(out, indent+l)
			}
		}
	}
	return out, nil
}

// renderMermaid draws flowcharts, sequence and ER diagrams as box-drawing
// text; other diagram types fall back to their source.
func renderMermaid(code string, w int) (lines []string) {
	fallback := func(reason string) []string {
		out := []string{dimStyle.Render("mermaid: " + safeText(reason))}
		for _, l := range splitLines(code) {
			out = append(out, gutterStyle.Render(ansi.Truncate(l, max(1, w), "…")))
		}
		return out
	}
	defer func() {
		if r := recover(); r != nil {
			lines = fallback("unsupported diagram")
		}
	}()
	cfg := diagram.DefaultConfig()
	cfg.MaxWidth = max(20, w)
	text, err := mermaid.RenderDiagram(code, cfg)
	if err != nil {
		kind, _, _ := strings.Cut(strings.TrimSpace(code), "\n")
		kind, _, _ = strings.Cut(strings.TrimSpace(kind), " ")
		switch kind {
		case "graph", "flowchart", "sequenceDiagram", "erDiagram":
			return fallback("could not parse " + kind)
		}
		return fallback(kind + " is not supported")
	}
	st := lipgloss.NewStyle().Foreground(colFg)
	for _, l := range splitLines(strings.TrimRight(text, "\n")) {
		lines = append(lines, st.Render(ansi.Truncate(strings.TrimRight(l, " "), max(1, w), "…")))
	}
	return lines
}

// fitCells returns the cell box an iw×ih pixel image occupies when fitted
// into w×h cells. Unless upscale is set, it is never drawn larger than its
// own pixel size, so icons in documents stay small.
func fitCells(iw, ih float64, w, h int, cw, ch float64, upscale bool) (cols, rows int, scale float64) {
	scale = math.Min(float64(w)*cw/iw, float64(h)*ch/ih)
	if !upscale {
		scale = math.Min(scale, 1)
	}
	cols = clamp(int(math.Ceil(iw*scale/cw)), 1, w)
	rows = clamp(int(math.Ceil(ih*scale/ch)), 1, h)
	return cols, rows, scale
}

// blockImageLines renders an embedded image with half blocks at the size
// it would take as a Kitty image, so both modes lay out alike.
func blockImageLines(cw, ch float64) imageBoxFunc {
	return func(_ int, img image.Image, w, h int) []string {
		b := img.Bounds()
		cols, rows, _ := fitCells(float64(b.Dx()), float64(b.Dy()), w, h, cw, ch, false)
		return renderImage(img, cols, rows)
	}
}

// mdImageBox is the largest cell box an embedded image may take in a w×h
// preview: the document margins and a line of context stay visible.
func mdImageBox(w, h int) (int, int) {
	return max(1, w-4), max(1, h-2)
}
