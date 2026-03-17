package tui

import (
	"fmt"
	"log"
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

type highlightAnimTickMsg struct{}
type spinnerTickMsg time.Time

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
	StatusOptions   []string
	StatusColIdx    int
	ChecksColIdx    int
	db              *db.DB
	states          []string
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

	selectorOpen   bool
	selectorCursor int
}

func NewModel(d *db.DB, states []string) *Model {
	m := &Model{
		ColumnDefs:         PatchworkColumns,
		StatusOptions:      states,
		StatusColIdx:       ColState,
		ChecksColIdx:       ColChecks,
		db:                 d,
		states:             states,
		highlightAnimating: true,
	}
	m.reloadData()
	return m
}

func NewModelWithData(
	columns []ColumnDef,
	rows []RowData,
	statusOptions []string,
	statusColIdx int,
) *Model {
	return &Model{
		ColumnDefs:         columns,
		RowData:            rows,
		StatusOptions:      statusOptions,
		StatusColIdx:       statusColIdx,
		ChecksColIdx:       -1,
		highlightAnimating: true,
	}
}

func (m *Model) reloadData() {
	if m.db == nil {
		return
	}
	rows, err := LoadFromDB(m.db, m.states)
	if err != nil {
		log.Printf("reload data: %v", err)
		return
	}
	m.mu.Lock()
	m.RowData = rows
	if m.selectedRow >= len(rows) && len(rows) > 0 {
		m.selectedRow = len(rows) - 1
	}
	m.mu.Unlock()
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
