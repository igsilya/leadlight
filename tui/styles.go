package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const (
	highlightAnimInterval = 20
	highlightAnimStep     = 0.1
	spinnerInterval       = 100

	subRowIndent   = " "
	scrollBuffer   = 2
	reservedLines  = 3
	indicatorWidth = 2
)

var spinnerFrames = []string{
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
}

type rgb struct{ r, g, b int }

func (c rgb) hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
}

func (c rgb) lerp(target rgb, t float64) rgb {
	return rgb{
		int(float64(c.r)*(1-t) + float64(target.r)*t),
		int(float64(c.g)*(1-t) + float64(target.g)*t),
		int(float64(c.b)*(1-t) + float64(target.b)*t),
	}
}

type bgColor struct{ bg, fg rgb }

var activeTheme *theme

var (
	headerStyle            lipgloss.Style
	separatorStyle         lipgloss.Style
	helpStyle              lipgloss.Style
	helpBrightStyle        lipgloss.Style
	helpDimStyle           lipgloss.Style
	statusStyle            lipgloss.Style
	normalOptionStyle      lipgloss.Style
	highlightedOptionStyle lipgloss.Style
	optionNumStyle         lipgloss.Style
	cellHighlightStyle     lipgloss.Style
	checksPassStyle        lipgloss.Style
	checksFailStyle        lipgloss.Style
	checksPendingStyle     lipgloss.Style
	checksZeroStyle        lipgloss.Style
	checkPassColor         string
	checkFailColor         string
	checkPendColor         string
	checkZeroColor         string
	mboxHeaderLabel        lipgloss.Style
	mboxHeaderValue        lipgloss.Style
	diffAddStyle           lipgloss.Style
	diffDelStyle           lipgloss.Style
	diffHunkStyle          lipgloss.Style
	diffHeaderStyle        lipgloss.Style
	quotedLineStyle        lipgloss.Style
	wrapIndicatorStyle     lipgloss.Style
	logLineStyle           lipgloss.Style
)

type gradientEntry struct{ bg, fg string }

var (
	gradientPalettes       = map[string][256]gradientEntry{}
	subRowGradientPalettes = map[string][256]gradientEntry{}
	termBg                 rgb
	termBgHex              string
)

type cachedBgStyle struct {
	row       lipgloss.Style
	rowFaint  lipgloss.Style
	checkPass lipgloss.Style
	checkFail lipgloss.Style
	checkPend lipgloss.Style
	checkZero lipgloss.Style
	bgHex     string
	fgHex     string
}

var bgStyles = map[string]*cachedBgStyle{}

func SetTheme(name string) {
	switch name {
	case "light":
		buildStyles(&lightTheme)
	case "dark":
		buildStyles(&darkTheme)
	}
}

func buildStyles(t *theme) {
	activeTheme = t
	fg := func(c string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	bold := func(c string) lipgloss.Style { return fg(c).Bold(true) }
	hlStyle := lipgloss.NewStyle().Bold(true).
		Background(lipgloss.Color(t.HighlightedOptionBg)).
		Foreground(lipgloss.Color(t.HighlightedOptionFg))

	headerStyle = bold(t.HeaderFg)
	separatorStyle = fg(t.SeparatorFg)
	helpStyle = fg(t.HelpFg)
	helpBrightStyle = fg(t.HelpBrightFg)
	helpDimStyle = fg(t.HelpDimFg)
	statusStyle = fg(t.StatusFg)
	normalOptionStyle = fg(t.NormalOptionFg)
	highlightedOptionStyle = hlStyle
	optionNumStyle = fg(t.OptionNumFg)
	cellHighlightStyle = hlStyle
	checksPassStyle = bold(t.ChecksPassFg)
	checksFailStyle = bold(t.ChecksFailFg)
	checksPendingStyle = bold(t.ChecksPendingFg)
	checksZeroStyle = fg(t.ChecksZeroFg)
	checkPassColor = t.ChecksPassFg
	checkFailColor = t.ChecksFailFg
	checkPendColor = t.ChecksPendingFg
	checkZeroColor = t.ChecksZeroFg
	mboxHeaderLabel = bold(t.MboxHeaderLabelFg)
	mboxHeaderValue = fg(t.MboxHeaderValueFg)
	diffAddStyle = fg(t.DiffAddFg)
	diffDelStyle = fg(t.DiffDelFg)
	diffHunkStyle = fg(t.DiffHunkFg)
	diffHeaderStyle = lipgloss.NewStyle().Bold(true)
	quotedLineStyle = fg(t.QuotedLineFg)
	wrapIndicatorStyle = fg(t.WrapIndicatorFg)
	logLineStyle = fg(t.LogLineFg)

	bgStyles = map[string]*cachedBgStyle{}
	gradientPalettes = map[string][256]gradientEntry{}
	subRowGradientPalettes = map[string][256]gradientEntry{}

	passFg := checksPassStyle.GetForeground()
	failFg := checksFailStyle.GetForeground()
	pendFg := checksPendingStyle.GetForeground()
	zeroFg := checksZeroStyle.GetForeground()

	for name, c := range t.BgColors {
		bgHex := c.bg.hex()
		fgHex := c.fg.hex()
		bgC := lipgloss.Color(bgHex)
		fgC := lipgloss.Color(fgHex)
		base := lipgloss.NewStyle().Background(bgC)

		bgStyles[name] = &cachedBgStyle{
			bgHex:     bgHex,
			fgHex:     fgHex,
			row:       base.Foreground(fgC),
			rowFaint:  base.Foreground(fgC).Faint(true),
			checkPass: base.Foreground(passFg).Bold(true),
			checkFail: base.Foreground(failFg).Bold(true),
			checkPend: base.Foreground(pendFg).Bold(true),
			checkZero: base.Foreground(zeroFg),
		}

		makePalette := func(target rgb) [256]gradientEntry {
			var p [256]gradientEntry
			for i := range p {
				tt := math.Pow(float64(i)/255.0, 0.35)
				bg := t.GradientStart.lerp(target, tt)
				fg := t.GradientFgStart.lerp(c.fg, tt)
				p[i] = gradientEntry{bg.hex(), fg.hex()}
			}
			return p
		}
		gradientPalettes[name] = makePalette(c.bg)
		subRowGradientPalettes[name] = makePalette(termBg)
	}
}

func detectTerminalBg() {
	c := termenv.ConvertToRGB(termenv.BackgroundColor())
	termBg = rgb{int(c.R * 255), int(c.G * 255), int(c.B * 255)}
	termBgHex = termBg.hex()
}

func init() {
	detectTerminalBg()
	if lipgloss.HasDarkBackground() {
		buildStyles(&darkTheme)
	} else {
		buildStyles(&lightTheme)
	}
}
