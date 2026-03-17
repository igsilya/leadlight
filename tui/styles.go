package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
)

const (
	highlightAnimInterval = 20
	highlightAnimStep     = 0.1
	spinnerInterval       = 100

	gradientStartR = 95
	gradientStartG = 0
	gradientStartB = 255

	subRowIndent   = "    "
	scrollBuffer   = 2
	reservedLines  = 3
	indicatorWidth = 2
)

var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	separatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	spinnerFrames  = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

	normalOptionStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	highlightedOptionStyle = lipgloss.NewStyle().
				Bold(true).
				Background(lipgloss.Color("57")).
				Foreground(lipgloss.Color("15"))
	optionNumStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	cellHighlightStyle = lipgloss.NewStyle().
				Bold(true).
				Background(lipgloss.Color("57")).
				Foreground(lipgloss.Color("15"))
	checksPassStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	checksFailStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	checksPendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	checksZeroStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type bgColor struct {
	r, g, b       int
	fgR, fgG, fgB int
}

var bgColors = map[string]bgColor{
	"yellow":   {0x55, 0x4d, 0x00, 0xff, 0xf0, 0x80},
	"white":    {0x3a, 0x3a, 0x3a, 0xee, 0xee, 0xee},
	"lightred": {0x55, 0x20, 0x20, 0xff, 0xb0, 0xb0},
	"darkred":  {0x8b, 0x10, 0x10, 0xff, 0xdd, 0xdd},
	"green":    {0x15, 0x50, 0x20, 0x90, 0xff, 0xa0},
	"grey":     {0x35, 0x35, 0x35, 0xcc, 0xcc, 0xcc},
	"black":    {0x12, 0x12, 0x12, 0x99, 0x99, 0x99},
}

type gradientEntry struct{ bg, fg string }

var gradientPalettes = map[string][256]gradientEntry{}

func init() {
	for name, c := range bgColors {
		var palette [256]gradientEntry
		for i := range palette {
			t := math.Pow(float64(i)/255.0, 0.35)
			r := int(float64(gradientStartR)*(1-t) + float64(c.r)*t)
			g := int(float64(gradientStartG)*(1-t) + float64(c.g)*t)
			b := int(float64(gradientStartB)*(1-t) + float64(c.b)*t)
			fgR := int(255.0*(1-t) + float64(c.fgR)*t)
			fgG := int(255.0*(1-t) + float64(c.fgG)*t)
			fgB := int(255.0*(1-t) + float64(c.fgB)*t)
			palette[i] = gradientEntry{
				bg: fmt.Sprintf("#%02x%02x%02x", r, g, b),
				fg: fmt.Sprintf("#%02x%02x%02x", fgR, fgG, fgB),
			}
		}
		gradientPalettes[name] = palette
	}
}
