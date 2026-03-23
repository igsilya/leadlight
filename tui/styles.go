package tui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const (
	// 10 steps * 20ms = ~200ms total animation. Tuned to feel snappy.
	highlightAnimInterval = 20
	highlightAnimStep     = 0.1
	spinnerInterval       = 100 // ~10 FPS, standard terminal spinner rate

	subRowIndent   = " "
	scrollBuffer   = 2 // rows kept visible below cursor when scrolling
	reservedLines  = 3 // header + separator + status bar
	indicatorWidth = 2 // "▸ " selection indicator
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
	checksWarnStyle        lipgloss.Style
	checksPendingStyle     lipgloss.Style // mbox view only
	checksZeroStyle        lipgloss.Style
	afrtStyle              lipgloss.Style
	afrtColor              string
	checkPassColor         string
	checkFailColor         string
	checkWarnColor         string
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
	gradientPalettes = map[string][256]gradientEntry{}
	termBg           rgb
	termBgHex        string
)

type cachedBgStyle struct {
	row       lipgloss.Style
	afrt      lipgloss.Style
	checkPass lipgloss.Style
	checkFail lipgloss.Style
	checkWarn lipgloss.Style
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
	checksWarnStyle = bold(t.ChecksWarnFg)
	checksPendingStyle = fg(t.ChecksPendingFg)
	checksZeroStyle = fg(t.ChecksZeroFg)
	afrtStyle = fg(t.AfrtFg).Bold(true)
	afrtColor = t.AfrtFg
	checkPassColor = t.ChecksPassFg
	checkFailColor = t.ChecksFailFg
	checkWarnColor = t.ChecksWarnFg
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

	passFg := checksPassStyle.GetForeground()
	failFg := checksFailStyle.GetForeground()
	warnFg := checksWarnStyle.GetForeground()
	zeroFg := checksZeroStyle.GetForeground()
	aFg := afrtStyle.GetForeground()

	for name, c := range t.BgColors {
		bgHex := c.bg.hex()
		fgHex := c.fg.hex()

		makeCachedStyle := func(bHex, fHex string) *cachedBgStyle {
			bC := lipgloss.Color(bHex)
			fC := lipgloss.Color(fHex)
			b := lipgloss.NewStyle().Background(bC)
			return &cachedBgStyle{
				bgHex:     bHex,
				fgHex:     fHex,
				row:       b.Foreground(fC),
				afrt:      b.Foreground(aFg).Bold(true),
				checkPass: b.Foreground(passFg).Bold(true),
				checkFail: b.Foreground(failFg).Bold(true),
				checkWarn: b.Foreground(warnFg).Bold(true),
				checkZero: b.Foreground(zeroFg),
			}
		}
		bgStyles[name] = makeCachedStyle(bgHex, fgHex)

		makePalette := func(targetBg, targetFg rgb) [256]gradientEntry {
			var p [256]gradientEntry
			for i := range p {
				// Gamma < 1 biases toward the target color, making the bright
				// highlight band narrow and the fade-out gradual.
				tt := math.Pow(float64(i)/255.0, 0.35)
				bg := t.GradientStart.lerp(targetBg, tt)
				fg := t.GradientFgStart.lerp(targetFg, tt)
				p[i] = gradientEntry{bg.hex(), fg.hex()}
			}
			return p
		}
		gradientPalettes[name] = makePalette(c.bg, c.fg)

		// Sub-rows get a dimmed variant of the parent color. The "sub:"
		// prefix lets the renderer look up the right style by prepending
		// "sub:" to the parent's color name (e.g., "sub:active").
		subBg := c.bg.lerp(t.SubRowBgAnchor, t.SubRowBgBlend)
		subFg := c.fg.lerp(t.SubRowFgAnchor, t.SubRowFgBlend)
		bgStyles["sub:"+name] = makeCachedStyle(subBg.hex(), subFg.hex())
		gradientPalettes["sub:"+name] = makePalette(subBg, subFg)
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
