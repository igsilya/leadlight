package tui

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"leadlight/db"
)

type ColumnDef struct {
	Title      string
	Percentage float64
}

type RowStyle struct {
	Foreground string
	Background string
	Bold       bool
	Italic     bool
}

func (rs RowStyle) lipgloss(faint bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if c, ok := bgColors[rs.Background]; ok {
		s = s.Background(lipgloss.Color(
			fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)))
		s = s.Foreground(lipgloss.Color(
			fmt.Sprintf("#%02x%02x%02x", c.fgR, c.fgG, c.fgB)))
	} else if rs.Foreground != "" {
		s = s.Foreground(lipgloss.Color(rs.Foreground))
	}
	if rs.Bold {
		s = s.Bold(true)
	}
	if rs.Italic {
		s = s.Italic(true)
	}
	if faint {
		s = s.Faint(true)
	}
	return s
}

type RowData struct {
	Data     []string
	Style    RowStyle
	SubRows  [][]string
	Expanded bool
}

type visibleItem struct {
	data      []string
	style     RowStyle
	isSubRow  bool
	parentIdx int
	subRowIdx int
	canExpand bool
}

type SyncUpdateMsg struct{}
type patchUpdateResultMsg struct{ err error }
type mboxResultMsg struct {
	patchID int
	content string
	err     error
}

type highlightAnimTickMsg struct{}
type spinnerTickMsg time.Time

type selectorMode int

const (
	selectorNone selectorMode = iota
	selectorState
	selectorDelegate
)

type viewMode int

const (
	viewTable viewMode = iota
	viewPatch
)

func highlightAnimTickCmd() tea.Cmd {
	return tea.Tick(
		time.Duration(highlightAnimInterval)*time.Millisecond,
		func(t time.Time) tea.Msg { return highlightAnimTickMsg{} })
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(
		time.Duration(spinnerInterval)*time.Millisecond,
		func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
}

type Model struct {
	ColumnDefs      []ColumnDef
	RowData         []RowData
	StatusColIdx    int
	ChecksColIdx    int
	db              *db.DB
	states          []string
	token           string
	selectedRow     int
	width           int
	height          int
	scrollOffset    int
	lastRowsVisible int
	mu              sync.Mutex
	status          string
	spinnerFrame    int

	highlightProgress  float64
	highlightAnimating bool

	selectorMode    selectorMode
	selectorCursor  int
	selectorOptions []string
	selectorIDs     []int
	selectorFilter  string

	selectedID string

	viewMode       viewMode
	viewingPatchID int
	viewportLines  []string
	viewportOffset int

	RequestMbox        func(patchID int)
	RequestPatchUpdate func(
		patchID int, state *string, delegateID *int,
	)
}

func NewModel(d *db.DB, states []string, token string) *Model {
	m := &Model{
		ColumnDefs:         PatchworkColumns,
		StatusColIdx:       ColState,
		ChecksColIdx:       ColChecks,
		db:                 d,
		states:             states,
		token:              token,
		highlightAnimating: true,
	}
	m.reloadData()
	return m
}

func NewModelWithData(
	columns []ColumnDef,
	rows []RowData,
	statusColIdx int,
) *Model {
	return &Model{
		ColumnDefs:         columns,
		RowData:            rows,
		StatusColIdx:       statusColIdx,
		ChecksColIdx:       -1,
		highlightAnimating: true,
	}
}

func (m *Model) reloadData() {
	if m.db == nil {
		return
	}

	expanded := map[string]bool{}
	for _, rd := range m.RowData {
		if rd.Expanded && len(rd.Data) > 0 {
			expanded[rd.Data[0]] = true
		}
	}

	seriesList := m.db.GetActiveSeries(m.states)
	rows := make([]RowData, 0, len(seriesList))
	for _, s := range seriesList {
		patches := m.db.GetPatchesForSeries(s.ID)
		row := seriesToRow(s, patches)
		sid := strconv.Itoa(s.ID)
		if expanded[sid] {
			row.Expanded = true
		}
		rows = append(rows, row)
	}

	m.mu.Lock()
	m.RowData = rows
	m.restoreSelection()
	m.mu.Unlock()
}

func (m *Model) restoreSelection() {
	items := m.getVisibleItems()
	for i, item := range items {
		if len(item.data) > 0 && item.data[0] == m.selectedID {
			m.selectedRow = i
			return
		}
	}
	if m.selectedRow >= len(items) && len(items) > 0 {
		m.selectedRow = len(items) - 1
	}
	m.updateSelectedID()
}

func (m *Model) updateSelectedID() {
	items := m.getVisibleItems()
	if m.selectedRow < len(items) && len(items[m.selectedRow].data) > 0 {
		m.selectedID = items[m.selectedRow].data[0]
	}
}

func (m *Model) Init() tea.Cmd {
	return highlightAnimTickCmd()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerTickMsg:
		if m.status != "" {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, spinnerTickCmd()
		}
		return m, nil

	case highlightAnimTickMsg:
		if !m.highlightAnimating {
			return m, nil
		}
		m.highlightProgress += highlightAnimStep
		if m.highlightProgress >= 1.0 {
			m.highlightProgress = 1.0
			m.highlightAnimating = false
			return m, nil
		}
		return m, highlightAnimTickCmd()

	case SyncUpdateMsg:
		m.reloadData()
		if m.viewMode == viewPatch &&
			len(m.viewportLines) == 1 &&
			m.viewportLines[0] == "Fetching..." {
			m.refreshViewport()
		}
		return m, nil

	case patchUpdateResultMsg:
		if msg.err != nil {
			log.Printf("TUI: patch update error: %v", msg.err)
			m.status = "Update failed: " + msg.err.Error()
		} else {
			log.Println("TUI: patch update success")
			m.status = ""
		}
		return m, nil

	case mboxResultMsg:
		log.Printf("TUI: mboxResultMsg patchID=%d err=%v",
			msg.patchID, msg.err)
		if msg.err != nil {
			m.viewportLines = []string{
				FormatMboxError("patch", msg.err)}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) refreshViewport() {
	if m.viewingPatchID == 0 || m.db == nil {
		return
	}
	row, err := m.db.GetPatch(m.viewingPatchID)
	if err != nil {
		log.Printf("TUI: refreshViewport: GetPatch(%d) error: %v",
			m.viewingPatchID, err)
		return
	}
	if row.MboxContent == "" {
		log.Printf("TUI: refreshViewport: %q mbox still empty",
			row.Name)
		return
	}
	log.Printf("TUI: refreshViewport: %q got %d bytes",
		row.Name, len(row.MboxContent))
	parsed := ParseMbox(row.MboxContent)
	formatted := FormatMbox(parsed, m.width)
	m.viewportLines = splitLines(formatted)
}

func splitLines(content string) []string {
	return strings.Split(content, "\n")
}

func (m *Model) getVisibleItems() []visibleItem {
	var items []visibleItem
	for i, rd := range m.RowData {
		items = append(items, visibleItem{
			data:      rd.Data,
			style:     rd.Style,
			isSubRow:  false,
			parentIdx: i,
			subRowIdx: -1,
			canExpand: len(rd.SubRows) > 0,
		})
		if rd.Expanded {
			subStyle := RowStyle{}
			for si, sub := range rd.SubRows {
				items = append(items, visibleItem{
					data:      sub,
					style:     subStyle,
					isSubRow:  true,
					parentIdx: i,
					subRowIdx: si,
				})
			}
		}
	}
	return items
}

func (m *Model) columnWidths() []int {
	if m.width == 0 {
		return nil
	}
	available := m.width - indicatorWidth
	widths := make([]int, len(m.ColumnDefs))
	for i, col := range m.ColumnDefs {
		widths[i] = int(float64(available) * col.Percentage)
	}
	return widths
}
