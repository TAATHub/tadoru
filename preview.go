package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	maxPreviewBytes   = 1 << 20
	maxHighlightBytes = 256 << 10
	// Decoded images are downscaled once on load so that re-fitting on
	// every terminal resize stays cheap.
	maxImageSide   = 2048
	maxImagePixels = 100_000_000
)

type previewKind int

const (
	pvNone previewKind = iota
	pvLoading
	pvDir
	pvText
	pvMarkdown
	pvImage
	pvBinary
	pvEmpty
	pvError
)

type previewContent struct {
	kind    previewKind
	path    string
	modTime time.Time
	size    int64

	text    []string
	raw     string
	mime    string
	entries []entry
	img     image.Image
	blocks  []mdBlock
	note    string
	err     error

	cacheW, cacheH int
	cacheTag       string
	cacheLines     []string
	cacheValid     bool
}

// sourceTag asks lines for a Markdown file's highlighted source.
const sourceTag = "source"

// lines renders the preview for a w×h box. tag names whatever imgFn depends
// on, so a new Kitty placement plan invalidates the cached lines.
func (p *previewContent) lines(w, h int, tag string, imgFn imageBoxFunc) []string {
	if p.cacheValid && p.cacheW == w && p.cacheH == h && p.cacheTag == tag {
		return p.cacheLines
	}
	var out []string
	switch p.kind {
	case pvLoading:
		out = []string{dimStyle.Render("Loading…")}
	case pvDir:
		if len(p.entries) == 0 {
			out = []string{dimStyle.Render("(empty directory)")}
		}
		for _, e := range p.entries {
			out = append(out, renderEntryLine(e, w, false, false, false))
		}
	case pvText:
		out = numberLines(p.text, w)
	case pvMarkdown:
		if tag == sourceTag {
			out = numberLines(p.text, w)
			break
		}
		var err error
		if out, err = renderMarkdownDoc(p.raw, p.blocks, w, h, imgFn); err != nil {
			out = numberLines(p.text, w)
		}
	case pvBinary:
		out = infoCard([]string{
			lipgloss.NewStyle().Foreground(colFg).Bold(true).Render("Binary file"),
			dimStyle.Render(p.mime),
			dimStyle.Render(humanSize(p.size)),
		}, w, h)
	case pvImage:
		out = renderImage(p.img, w, h)
	case pvEmpty:
		out = []string{dimStyle.Render("(" + p.note + ")")}
	case pvError:
		out = []string{errStyle.Render(safeText(p.err.Error()))}
	}
	p.cacheW, p.cacheH, p.cacheTag, p.cacheLines, p.cacheValid = w, h, tag, out, true
	return out
}

func numberLines(src []string, w int) []string {
	gutter := len(strconv.Itoa(len(src)))
	cw := w - gutter - 1
	if cw < 1 {
		return src
	}
	out := make([]string, 0, len(src))
	for i, line := range src {
		num := gutterStyle.Render(fmt.Sprintf("%*d", gutter, i+1)) + " "
		out = append(out, num+ansi.Truncate(line, cw, "…"))
	}
	return out
}

func buildPreview(path string) *previewContent {
	p := &previewContent{path: path}
	st, err := os.Stat(path)
	if err != nil {
		p.kind, p.err = pvError, err
		return p
	}
	p.modTime, p.size = st.ModTime(), st.Size()

	if st.IsDir() {
		p.entries, p.err = readEntries(path)
		if p.err != nil {
			p.kind = pvError
			return p
		}
		p.kind = pvDir
		p.note = fmt.Sprintf("%d items", len(p.entries))
		return p
	}
	// Reading a FIFO or device would block the loader forever.
	if !st.Mode().IsRegular() {
		p.kind, p.note = pvEmpty, "special file: "+st.Mode().Type().String()
		return p
	}
	if st.Size() == 0 {
		p.kind, p.note = pvEmpty, "empty file"
		return p
	}

	if isImageExt(path) {
		img, w, h, err := decodeImage(path)
		if err == nil {
			p.kind, p.img = pvImage, img
			p.note = fmt.Sprintf("%d×%d", w, h)
			return p
		}
		p.note = "image: " + err.Error()
	}

	f, err := os.Open(path)
	if err != nil {
		p.kind, p.err = pvError, err
		return p
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxPreviewBytes))
	if err != nil {
		p.kind, p.err = pvError, err
		return p
	}
	truncated := st.Size() > int64(len(buf))

	if isBinary(buf) {
		p.kind = pvBinary
		p.mime = strings.SplitN(http.DetectContentType(buf), ";", 2)[0]
		return p
	}

	p.kind = pvText
	text := sanitize(string(buf))
	if isMarkdownExt(path) {
		p.kind = pvMarkdown
		p.raw, p.blocks = extractBlocks(path, text)
	}
	if len(buf) <= maxHighlightBytes {
		p.text = highlight(path, text)
	} else {
		p.text = splitLines(text)
	}
	if truncated {
		p.note = "first " + humanSize(int64(len(buf)))
	}
	return p
}

func isImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return true
	}
	return false
}

// infoCard centres a few short lines in a w×h box.
func infoCard(lines []string, w, h int) []string {
	out := make([]string, max(0, (h-len(lines))/2))
	for _, l := range lines {
		out = append(out, strings.Repeat(" ", max(0, (w-ansi.StringWidth(l))/2))+l)
	}
	return out
}

func isMarkdownExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

func renderMarkdown(src string, w int) ([]string, error) {
	r, err := glamour.NewTermRenderer(
		// Auto style would query the terminal, which races with Bubble Tea's input reader.
		glamour.WithStyles(glamourStyle()),
		// The document style adds a 2-cell margin on each side.
		glamour.WithWordWrap(max(10, w-4)),
	)
	if err != nil {
		return nil, err
	}
	out, err := r.Render(src)
	if err != nil {
		return nil, err
	}
	// glamour only breaks at spaces, so CJK paragraphs overflow the box.
	var lines []string
	for _, line := range splitLines(strings.Trim(out, "\n")) {
		if ansi.StringWidth(line) > w {
			line = ansi.Hardwrap(line, w, true)
		}
		lines = append(lines, strings.Split(line, "\n")...)
	}
	return lines, nil
}

func isBinary(buf []byte) bool {
	sample := buf[:min(len(buf), 8192)]
	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}
	// The sample may end in the middle of a multi-byte rune.
	for i := 0; i < utf8.UTFMax && len(sample) > 0 && !utf8.Valid(sample); i++ {
		sample = sample[:len(sample)-1]
	}
	return !utf8.Valid(sample)
}

// sanitize strips control characters so file contents cannot inject
// escape sequences into the terminal.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\r':
			return -1
		case r < 0x20, r == 0x7f, r >= 0x80 && r < 0xa0:
			return '�'
		}
		return r
	}, s)
}

func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func chromaStyle() *chroma.Style {
	return chromastyles.Get(current.chroma)
}

// highlight emits SGR codes per token and splits tokens at newlines itself,
// so every line is self-contained even inside multi-line comments/strings.
func highlight(path, text string) []string {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Analyse(text)
	}
	if lexer == nil {
		return splitLines(text)
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return splitLines(text)
	}
	style := chromaStyle()
	var lines []string
	var cur strings.Builder
	for tok := it(); tok != chroma.EOF; tok = it() {
		sgr := sgrFor(style.Get(tok.Type))
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				lines = append(lines, cur.String())
				cur.Reset()
			}
			if part == "" {
				continue
			}
			if sgr == "" {
				cur.WriteString(part)
			} else {
				cur.WriteString(sgr + part + "\x1b[0m")
			}
		}
	}
	lines = append(lines, cur.String())
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func sgrFor(e chroma.StyleEntry) string {
	var codes []string
	if e.Colour.IsSet() {
		codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", e.Colour.Red(), e.Colour.Green(), e.Colour.Blue()))
	}
	if e.Bold == chroma.Yes {
		codes = append(codes, "1")
	}
	if e.Italic == chroma.Yes {
		codes = append(codes, "3")
	}
	if e.Underline == chroma.Yes {
		codes = append(codes, "4")
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

func decodeImage(path string) (image.Image, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, 0, 0, err
	}
	if cfg.Width*cfg.Height > maxImagePixels {
		return nil, 0, 0, fmt.Errorf("too large (%d×%d)", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, 0, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, 0, 0, err
	}
	if b := img.Bounds(); b.Dx() > maxImageSide || b.Dy() > maxImageSide {
		img = scaleToFit(img, maxImageSide, maxImageSide)
	}
	return img, cfg.Width, cfg.Height, nil
}

func scaleToFit(img image.Image, maxW, maxH int) *image.RGBA {
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	scale := math.Min(float64(maxW)/float64(iw), float64(maxH)/float64(ih))
	dw := max(1, int(float64(iw)*scale))
	dh := max(1, int(float64(ih)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

// Composited behind transparent pixels; set from the theme background.
var imageBg color.RGBA

// renderImage draws two pixels per cell with the upper-half block, using
// the foreground for the top pixel and the background for the bottom one.
func renderImage(img image.Image, w, h int) []string {
	if img == nil || w < 1 || h < 1 {
		return nil
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return nil
	}
	dst := scaleToFit(img, w, h*2)
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	pad := strings.Repeat(" ", (w-dw)/2)
	lines := make([]string, 0, (dh+1)/2)
	for y := 0; y < dh; y += 2 {
		var sb strings.Builder
		sb.WriteString(pad)
		for x := 0; x < dw; x++ {
			t := blend(dst.RGBAAt(x, y))
			if y+1 < dh {
				bt := blend(dst.RGBAAt(x, y+1))
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", t.R, t.G, t.B, bt.R, bt.G, bt.B)
			} else {
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%d;49m▀", t.R, t.G, t.B)
			}
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}
	return lines
}

// blend composites a premultiplied RGBA pixel over imageBg.
func blend(c color.RGBA) color.RGBA {
	inv := 255 - uint32(c.A)
	return color.RGBA{
		R: uint8(uint32(c.R) + uint32(imageBg.R)*inv/255),
		G: uint8(uint32(c.G) + uint32(imageBg.G)*inv/255),
		B: uint8(uint32(c.B) + uint32(imageBg.B)*inv/255),
		A: 0xff,
	}
}
