package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	previewDebounce = 30 * time.Millisecond
	maxCacheEntries = 128
	maxCachedImages = 8
	wheelLines      = 3
	maxParentWidth  = 60
	maxCurrentWidth = 90
)

type previewTickMsg struct {
	id   int
	path string
}

type previewLoadedMsg struct {
	id int
	p  *previewContent
}

type layout struct {
	pw, cw, vw int
	h          int
	cx, vx     int
}

type Model struct {
	width, height int

	cwd           string
	allEntries    []entry
	entries       []entry
	err           error
	parentEntries []entry

	cursor, offset int
	// Remembers the selected name per directory so that going back and
	// forth restores the previous position.
	lastSelected map[string]string

	preview  *previewContent
	pvOffset int
	reqID    int
	cache    map[string]*previewContent
	// Decoded images are large, so they are evicted separately.
	cachedImages []string

	filter    string
	searching bool

	sel selection

	imgMode      imageMode
	cellW, cellH float64
	// kitty is the set of images on the terminal, sent for kittyOn; plan is
	// the latest placement, whose ids the preview lines already use while
	// its payloads are being encoded for kittyWant.
	kitty     []*kittyImage
	kittyOn   string
	kittyWant string
	plan      []*kittyImage
	planKey   string
	kittySeq  uint32

	// mdSource shows Markdown as its source instead of rendered; it sticks
	// across files so a run of documents can be read the same way.
	mdSource bool

	status string
}

func newModel(dir string) *Model {
	m := &Model{
		cwd:          dir,
		lastSelected: map[string]string{},
		cache:        map[string]*previewContent{},
		imgMode:      detectImageMode(),
	}
	m.cellW, m.cellH = cellPixelSize()
	m.loadDir("")
	return m
}

func (m *Model) Init() tea.Cmd {
	return m.requestPreview()
}

func (m *Model) hasParent() bool {
	return filepath.Dir(m.cwd) != m.cwd
}

func (m *Model) loadDir(selectName string) {
	m.allEntries, m.err = readEntries(m.cwd)
	m.entries = filterEntries(m.allEntries, m.filter)
	m.parentEntries = nil
	if m.hasParent() {
		m.parentEntries, _ = readEntries(filepath.Dir(m.cwd))
	}
	if selectName == "" {
		selectName = m.lastSelected[m.cwd]
	}
	m.cursor, m.offset = 0, 0
	for i, e := range m.entries {
		if e.name == selectName {
			m.cursor = i
			break
		}
	}
	m.ensureVisible()
}

func (m *Model) selected() *entry {
	if m.cursor < 0 || m.cursor >= len(m.entries) {
		return nil
	}
	return &m.entries[m.cursor]
}

func (m *Model) layout() layout {
	pOuter := clamp(m.width*20/100, 10, maxParentWidth)
	cOuter := clamp(m.width*30/100, 16, maxCurrentWidth)
	vOuter := max(3, m.width-pOuter-cOuter)
	return layout{
		pw: pOuter - 2, cw: cOuter - 2, vw: vOuter - 2,
		h:  max(1, m.height-4),
		cx: pOuter, vx: pOuter + cOuter,
	}
}

func (m *Model) ensureVisible() {
	h := m.layout().h
	if m.height == 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = clamp(m.offset, 0, max(0, len(m.entries)-h))
}

func (m *Model) parentCursorOffset() (int, int) {
	name := filepath.Base(m.cwd)
	idx := 0
	for i, e := range m.parentEntries {
		if e.name == name {
			idx = i
			break
		}
	}
	h := m.layout().h
	return idx, clamp(idx-h/2, 0, max(0, len(m.parentEntries)-h))
}

func (m *Model) moveTo(i int) tea.Cmd {
	if len(m.entries) == 0 {
		return nil
	}
	i = clamp(i, 0, len(m.entries)-1)
	if i == m.cursor && m.preview != nil {
		return nil
	}
	m.cursor = i
	m.ensureVisible()
	m.lastSelected[m.cwd] = m.entries[i].name
	return m.requestPreview()
}

func filterEntries(entries []entry, q string) []entry {
	if q == "" {
		return entries
	}
	q = strings.ToLower(q)
	var out []entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.name), q) {
			out = append(out, e)
		}
	}
	return out
}

func (m *Model) setFilter(q string) tea.Cmd {
	name := ""
	if sel := m.selected(); sel != nil {
		name = sel.name
	}
	m.filter = q
	m.entries = filterEntries(m.allEntries, q)
	m.cursor, m.offset = 0, 0
	for i, e := range m.entries {
		if e.name == name {
			m.cursor = i
			break
		}
	}
	m.ensureVisible()
	return m.requestPreview()
}

func (m *Model) changeDir(dir, selectName string) tea.Cmd {
	m.cwd = dir
	m.filter, m.searching = "", false
	m.loadDir(selectName)
	return m.requestPreview()
}

func (m *Model) enter() tea.Cmd {
	sel := m.selected()
	if sel == nil || !sel.isDir {
		return nil
	}
	return m.changeDir(sel.path, "")
}

func (m *Model) back() tea.Cmd {
	if !m.hasParent() {
		return nil
	}
	return m.changeDir(filepath.Dir(m.cwd), filepath.Base(m.cwd))
}

func (m *Model) requestPreview() tea.Cmd {
	m.pvOffset = 0
	m.sel = selection{}
	sel := m.selected()
	if sel == nil {
		m.preview = nil
		return nil
	}
	if c, ok := m.cache[sel.path]; ok {
		if st, err := os.Stat(sel.path); err == nil && st.ModTime().Equal(c.modTime) && st.Size() == c.size {
			m.preview = c
			return m.ensureKitty()
		}
	}
	m.reqID++
	id, path := m.reqID, sel.path
	m.preview = &previewContent{kind: pvLoading, path: path}
	return tea.Tick(previewDebounce, func(time.Time) tea.Msg {
		return previewTickMsg{id: id, path: path}
	})
}

func (m *Model) storeCache(p *previewContent) {
	if p.kind == pvError || p.kind == pvLoading {
		return
	}
	if len(m.cache) >= maxCacheEntries {
		m.cache = map[string]*previewContent{}
		m.cachedImages = nil
	}
	key := p.path
	if p.kind == pvImage {
		m.cachedImages = append(m.cachedImages, key)
		if len(m.cachedImages) > maxCachedImages {
			delete(m.cache, m.cachedImages[0])
			m.cachedImages = m.cachedImages[1:]
		}
	}
	m.cache[key] = p
}

func (m *Model) usesKitty() bool {
	if m.imgMode != imageKitty || m.preview == nil {
		return false
	}
	return m.preview.kind == pvImage || (m.preview.kind == pvMarkdown && !m.mdSource && countImages(m.preview.blocks) > 0)
}

// ensureKitty plans and starts encoding the current preview's images for the
// current box size unless that exact transmission is already on the terminal.
func (m *Model) ensureKitty() tea.Cmd {
	if !m.usesKitty() {
		return nil
	}
	l := m.layout()
	key := kittyKey(m.preview, l.vw, l.h)
	if m.kittyOn == key || m.kittyWant == key {
		return nil
	}
	m.kittyWant = key
	m.plan, m.planKey = nil, key
	nextID := func() uint32 {
		m.kittySeq++
		// The id lives in the 24-bit foreground colour; mixing in the pid
		// keeps several instances in one terminal from clobbering each other.
		return uint32(os.Getpid()&0xffff)<<8 | (m.kittySeq%255 + 1)
	}
	if m.preview.kind == pvImage {
		m.plan = append(m.plan, planKitty(m.preview.img, key, nextID(), l.vw, l.h, m.cellW, m.cellH, true))
	} else {
		w, h := mdImageBox(l.vw, l.h)
		for _, b := range m.preview.blocks {
			if b.kind == mdImage {
				m.plan = append(m.plan, planKitty(b.img, key, nextID(), w, h, m.cellW, m.cellH, false))
			}
		}
	}
	jobs := make([]kittyImage, len(m.plan))
	for i, k := range m.plan {
		jobs[i] = *k
	}
	return func() tea.Msg {
		msg := kittyReadyMsg{key: key}
		for _, j := range jobs {
			if k := encodeKitty(j); k != nil {
				msg.imgs = append(msg.imgs, k)
			}
		}
		return msg
	}
}

func (m *Model) clearKitty() {
	for _, k := range m.kitty {
		os.Stdout.WriteString(kittyDelete(k.id))
	}
	m.kitty, m.kittyOn = nil, ""
}

// markdownImages lays out embedded images as Kitty placeholders from the
// current plan, or as half blocks when Kitty graphics are unavailable.
func (m *Model) markdownImages() (string, imageBoxFunc) {
	if m.imgMode != imageKitty {
		return "", blockImageLines(m.cellW, m.cellH)
	}
	l := m.layout()
	plan, tag := m.plan, ""
	if m.planKey != kittyKey(m.preview, l.vw, l.h) || len(plan) == 0 {
		plan = nil
	} else {
		// Ids change when the same document is planned again.
		tag = fmt.Sprintf("%s#%d", m.planKey, plan[0].id)
	}
	byBlock := map[int]*kittyImage{}
	n := 0
	for i, b := range m.preview.blocks {
		if b.kind == mdImage && n < len(plan) {
			byBlock[i] = plan[n]
			n++
		}
	}
	return tag, func(i int, _ image.Image, _, _ int) []string {
		if k := byBlock[i]; k != nil {
			return k.placeholderLines(k.cols)
		}
		return nil
	}
}

func (m *Model) previewLines() []string {
	if m.preview == nil {
		return nil
	}
	l := m.layout()
	switch {
	case m.usesKitty() && m.preview.kind == pvImage:
		// While a newer version of the same image is being encoded, keep
		// showing the one already on the terminal instead of flashing.
		if len(m.kitty) == 1 && strings.HasPrefix(m.kittyOn, m.preview.path+"|") {
			return m.kitty[0].placeholderLines(l.vw)
		}
		return []string{dimStyle.Render("Loading…")}
	case m.showsSource():
		return m.preview.lines(l.vw, l.h, sourceTag, nil)
	case m.preview.kind == pvMarkdown && len(m.preview.blocks) > 0:
		tag, fn := m.markdownImages()
		return m.preview.lines(l.vw, l.h, tag, fn)
	}
	return m.preview.lines(l.vw, l.h, "", nil)
}

// showsSource reports whether the preview is line-numbered source text.
func (m *Model) showsSource() bool {
	return m.preview != nil && (m.preview.kind == pvText || (m.preview.kind == pvMarkdown && m.mdSource))
}

func (m *Model) scrollPreview(delta int) {
	maxOff := max(0, len(m.previewLines())-m.layout().h)
	m.pvOffset = clamp(m.pvOffset+delta, 0, maxOff)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.cellW, m.cellH = cellPixelSize()
		m.ensureVisible()
		m.scrollPreview(0)
		return m, m.ensureKitty()

	case kittyReadyMsg:
		if msg.key != m.kittyWant {
			return m, nil
		}
		m.kittyWant = ""
		m.clearKitty()
		// os.File serialises writes, so this cannot interleave with a
		// renderer flush.
		for _, k := range msg.imgs {
			os.Stdout.WriteString(k.payload)
		}
		m.kitty, m.kittyOn = msg.imgs, msg.key
		return m, nil

	case previewTickMsg:
		if msg.id != m.reqID {
			return m, nil
		}
		id, path := msg.id, msg.path
		return m, func() tea.Msg {
			return previewLoadedMsg{id: id, p: buildPreview(path)}
		}

	case previewLoadedMsg:
		m.storeCache(msg.p)
		if msg.id == m.reqID {
			m.preview = msg.p
			m.scrollPreview(0)
			return m, m.ensureKitty()
		}
		return m, nil

	case tea.MouseMsg:
		return m, m.handleMouse(msg)

	case tea.KeyMsg:
		m.status = ""
		if m.searching {
			return m, m.handleSearchKey(msg)
		}
		// Fast key repeat can coalesce several runes into a single KeyMsg.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			var cmds []tea.Cmd
			for i, r := range msg.Runes {
				// Once "/" starts a search, the rest of the batch is the query.
				if m.searching {
					cmds = append(cmds, m.setFilter(m.filter+string(msg.Runes[i:])))
					break
				}
				cmds = append(cmds, m.handleKey(string(r)))
			}
			return m, tea.Batch(cmds...)
		}
		return m, m.handleKey(msg.String())
	}
	return m, nil
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.searching = false
		return m.setFilter("")
	case tea.KeyEnter:
		m.searching = false
		return nil
	case tea.KeyBackspace:
		r := []rune(m.filter)
		if len(r) == 0 {
			m.searching = false
			return nil
		}
		return m.setFilter(string(r[:len(r)-1]))
	case tea.KeyCtrlU:
		return m.setFilter("")
	case tea.KeyDown, tea.KeyCtrlN:
		return m.moveTo(m.cursor + 1)
	case tea.KeyUp, tea.KeyCtrlP:
		return m.moveTo(m.cursor - 1)
	case tea.KeyCtrlC:
		return tea.Quit
	case tea.KeyRunes, tea.KeySpace:
		return m.setFilter(m.filter + string(msg.Runes))
	}
	return nil
}

func (m *Model) openSelected() {
	if sel := m.selected(); sel != nil {
		if err := openDefault(sel.path); err != nil {
			m.status = "open: " + err.Error()
		} else {
			m.status = "Opened " + sel.name
		}
	}
}

func (m *Model) handleKey(key string) tea.Cmd {
	switch key {
	case "q", "ctrl+c":
		return tea.Quit
	case "o":
		m.openSelected()
	case "c":
		if sel := m.selected(); sel != nil {
			if err := copyToClipboard(sel.path); err != nil {
				m.status = "copy: " + err.Error()
			} else {
				m.status = "Copied " + abbrevHome(sel.path)
			}
		}
	case "r":
		if m.preview != nil && m.preview.kind == pvMarkdown {
			m.mdSource = !m.mdSource
			m.pvOffset = 0
			m.sel = selection{}
			return m.ensureKitty()
		}
	case "O":
		if sel := m.selected(); sel != nil {
			if err := revealInFileManager(sel.path); err != nil {
				m.status = "reveal: " + err.Error()
			} else {
				m.status = "Revealed " + sel.name + " in Finder"
			}
		}
	case "/":
		m.searching = true
	case "down":
		return m.moveTo(m.cursor + 1)
	case "up":
		return m.moveTo(m.cursor - 1)
	case "left":
		return m.back()
	case "right":
		return m.enter()
	}
	return nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	l := m.layout()
	inPreview := msg.X >= l.vx
	inCurrent := msg.X >= l.cx && msg.X < l.vx
	// Row 0 is the header and row 1 the top border.
	row := msg.Y - 2

	if m.sel.dragging {
		switch msg.Action {
		case tea.MouseActionMotion:
			// Dragging past the top or bottom edge scrolls the preview.
			if row < 0 {
				m.scrollPreview(-1)
			} else if row >= l.h {
				m.scrollPreview(1)
			}
			m.sel.head = m.previewPoint(msg.X, row)
			return nil
		case tea.MouseActionRelease:
			m.sel.head = m.previewPoint(msg.X, row)
			m.finishSelection()
			return nil
		}
	}

	switch msg.Button {
	case tea.MouseButtonWheelDown, tea.MouseButtonWheelUp:
		d := wheelLines
		if msg.Button == tea.MouseButtonWheelUp {
			d = -d
		}
		if inPreview {
			m.scrollPreview(d)
			return nil
		}
		if inCurrent {
			return m.moveTo(m.cursor + d/wheelLines)
		}
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress || row < 0 || row >= l.h {
			return nil
		}
		switch {
		case inPreview:
			sel := m.selected()
			if sel == nil || m.preview == nil {
				return nil
			}
			// A directory preview behaves like the next column: clicking an
			// entry descends into it with that entry selected.
			if m.preview.kind == pvDir {
				if idx := m.pvOffset + row; idx < len(m.preview.entries) {
					return m.changeDir(sel.path, m.preview.entries[idx].name)
				}
				return nil
			}
			if selectable(m.preview) {
				p := m.previewPoint(msg.X, row)
				m.sel = selection{active: true, dragging: true, anchor: p, head: p}
			}
		case inCurrent:
			if idx := m.offset + row; idx < len(m.entries) {
				return m.moveTo(idx)
			}
		default:
			_, off := m.parentCursorOffset()
			if idx := off + row; idx < len(m.parentEntries) {
				e := m.parentEntries[idx]
				return m.changeDir(filepath.Dir(m.cwd), e.name)
			}
		}
	}
	return nil
}

func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	l := m.layout()

	var parentBody []string
	parentTitle := ""
	if m.hasParent() {
		idx, off := m.parentCursorOffset()
		parentBody = renderList(m.parentEntries, idx, off, l.pw, l.h, false, false)
		parentTitle = filepath.Base(filepath.Dir(m.cwd))
	}
	parentBox := box(parentTitle, "", parentBody, l.pw, l.h, false)

	var curBody []string
	switch {
	case m.err != nil:
		curBody = []string{errStyle.Render(safeText(m.err.Error()))}
	case len(m.entries) == 0 && m.filter != "":
		curBody = []string{dimStyle.Render("(no match)")}
	case len(m.entries) == 0:
		curBody = []string{dimStyle.Render("(empty)")}
	default:
		curBody = renderList(m.entries, m.cursor, m.offset, l.cw, l.h, true, true)
	}
	curFooter := ""
	if len(m.entries) > 0 {
		curFooter = fmt.Sprintf("%d/%d", m.cursor+1, len(m.entries))
	}
	curTitle := filepath.Base(m.cwd)
	if m.filter != "" {
		curTitle += " /" + m.filter
	}
	curBox := box(curTitle, curFooter, curBody, l.cw, l.h, true)

	pvTitle, pvFooter := "", ""
	var pvBody []string
	if sel := m.selected(); sel != nil && m.preview != nil {
		pvTitle = sel.name
		lines := m.previewLines()
		m.pvOffset = clamp(m.pvOffset, 0, max(0, len(lines)-l.h))
		pvBody = lines[m.pvOffset:min(len(lines), m.pvOffset+l.h)]
		if m.sel.active {
			pvBody = append([]string(nil), pvBody...)
			for i := range pvBody {
				if c0, c1, ok := m.sel.colsFor(m.pvOffset + i); ok {
					pvBody[i] = highlightSelection(pvBody[i], c0, c1)
				}
			}
		}
		var parts []string
		if m.preview.note != "" {
			parts = append(parts, m.preview.note)
		}
		if len(lines) > l.h {
			pct := 100 * (m.pvOffset + l.h) / len(lines)
			parts = append(parts, fmt.Sprintf("%d-%d/%d %d%%", m.pvOffset+1, m.pvOffset+len(pvBody), len(lines), pct))
		}
		pvFooter = strings.Join(parts, " · ")
	}
	pvBox := box(pvTitle, pvFooter, pvBody, l.vw, l.h, false)

	cols := lipgloss.JoinHorizontal(lipgloss.Top, parentBox, curBox, pvBox)
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), cols, m.renderFooter())
}

func (m *Model) renderHeader() string {
	left := headerStyle.Render("tadoru") + " " + pathStyle.Render(safeText(abbrevHome(m.cwd)))
	return ansi.Truncate(left, m.width, "")
}

func (m *Model) renderFooter() string {
	left := ""
	if m.searching {
		left = " /" + safeText(m.filter) + "▏"
		right := "enter confirm  esc clear "
		gap := max(1, m.width-ansi.StringWidth(left)-ansi.StringWidth(right))
		return pathStyle.Render(left) + dimStyle.Render(strings.Repeat(" ", gap)+right)
	}
	if sel := m.selected(); sel != nil {
		left = fmt.Sprintf(" %s  %s  %s", sel.mode.String(), humanSize(sel.size), sel.modTime.Format("2006-01-02 15:04"))
	}
	var right string
	switch {
	case m.status != "":
		right = safeText(m.status)
	default:
		right = "/ search  c copy path  o open  O finder  q quit"
		if m.preview != nil && m.preview.kind == pvMarkdown {
			mode := "source"
			if m.mdSource {
				mode = "rendered"
			}
			right = "r " + mode + "  " + right
		}
	}
	right = ansi.Truncate(right, max(0, m.width-ansi.StringWidth(left)-2), "…") + " "
	gap := max(1, m.width-ansi.StringWidth(left)-ansi.StringWidth(right))
	return dimStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m *Model) previewPoint(x, row int) selPoint {
	l := m.layout()
	lines := m.previewLines()
	line := clamp(m.pvOffset+clamp(row, 0, l.h-1), 0, max(0, len(lines)-1))
	// x includes the left border of the preview box.
	return selPoint{line: line, col: clamp(x-l.vx-1, 0, l.vw-1)}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
