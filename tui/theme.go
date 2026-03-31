package tui

type theme struct {
	BgColors map[string]bgColor

	GradientStart   rgb
	GradientFgStart rgb

	SubRowBgAnchor rgb
	SubRowFgAnchor rgb
	SubRowBgBlend  float64
	SubRowFgBlend  float64

	HeaderFg, SeparatorFg           string
	HelpBrightFg, HelpFg, HelpSepFg string
	HelpDimFg                       string
	StatusFg                        string

	NormalOptionFg      string
	HighlightedOptionBg string
	HighlightedOptionFg string
	OptionNumFg         string

	AfrtFg string // non-zero values in C and A F R T columns

	ChecksPassFg, ChecksFailFg string
	ChecksWarnFg, ChecksZeroFg string
	ChecksPendingFg            string // mbox view only (truly pending checks)

	MboxHeaderLabelFg, MboxHeaderValueFg string
	DiffAddFg, DiffDelFg, DiffHunkFg     string
	QuotedLineFg, WrapIndicatorFg        string

	LogLineFg  string
	ApplyLogFg string // bright color for [apply] log lines
}

// Dark theme FG colors inspired by the Catppuccin Mocha palette.
// https://github.com/catppuccin/catppuccin (MIT license)
var darkTheme = theme{
	BgColors: map[string]bgColor{
		// warm ember bg, soft golden cream text
		"aging": {rgb{0x2a, 0x25, 0x20}, rgb{0xf9, 0xe2, 0xaf}},
		// twilight blue-grey bg, cool lavender text
		"active": {rgb{0x2a, 0x2a, 0x30}, rgb{0xcd, 0xd6, 0xf4}},
		// dusky plum bg, bright rose-pink text
		"pending": {rgb{0x2a, 0x20, 0x30}, rgb{0xf3, 0x8b, 0xa8}},
		// deep wine bg, vivid true red text
		"overdue": {rgb{0x2d, 0x18, 0x20}, rgb{0xf0, 0x58, 0x58}},
		// dark forest bg, fresh mint-green text
		"reviewed": {rgb{0x1a, 0x28, 0x20}, rgb{0xa6, 0xe3, 0xa1}},
		// charcoal bg, silver text
		"closed": {rgb{0x35, 0x35, 0x35}, rgb{0xcc, 0xcc, 0xcc}},
		// dark blue-grey bg, bright cool slate text
		"stale": {rgb{0x1a, 0x1a, 0x24}, rgb{0xb8, 0xc0, 0xd8}},
	},
	GradientStart:   rgb{95, 0, 255},    // electric violet
	GradientFgStart: rgb{255, 255, 255}, // pure white

	SubRowBgAnchor: rgb{0x10, 0x10, 0x10}, // near-black
	SubRowFgAnchor: rgb{0x40, 0x40, 0x40}, // dark charcoal
	SubRowBgBlend:  0.4,
	SubRowFgBlend:  0.25,

	HeaderFg:    "15",  // bright white
	SeparatorFg: "240", // mid-grey

	HelpBrightFg: "250",     // white-grey (keys)
	HelpFg:       "#7f849c", // muted blue-grey (descriptions)
	HelpSepFg:    "243",     // neutral mid-grey (separators, brackets)
	HelpDimFg:    "236",     // very dark grey (inactive pane)

	StatusFg: "214", // warm amber

	NormalOptionFg:      "255", // near-white
	HighlightedOptionBg: "57",  // deep indigo
	HighlightedOptionFg: "15",  // bright white
	OptionNumFg:         "250", // light grey

	AfrtFg: "147", // soft lavender

	ChecksPassFg:    "34",  // forest green
	ChecksFailFg:    "196", // bright red
	ChecksWarnFg:    "214", // warm amber
	ChecksZeroFg:    "240", // mid-grey
	ChecksPendingFg: "247", // light grey (mbox view: incomplete checks)

	MboxHeaderLabelFg: "15",  // bright white
	MboxHeaderValueFg: "252", // near-white
	DiffAddFg:         "34",  // forest green
	DiffDelFg:         "196", // bright red
	DiffHunkFg:        "6",   // dark cyan
	QuotedLineFg:      "168", // dusty rose
	WrapIndicatorFg:   "242", // dim grey

	LogLineFg:  "245",     // soft grey
	ApplyLogFg: "#cdd6f4", // catppuccin Text — bright, clearly readable
}

var lightTheme = theme{
	BgColors: map[string]bgColor{
		// pale butter bg, dark olive text
		"aging": {rgb{0xff, 0xf8, 0xd0}, rgb{0x55, 0x4d, 0x00}},
		// soft cloud bg, dark charcoal text
		"active": {rgb{0xe8, 0xe8, 0xe8}, rgb{0x33, 0x33, 0x33}},
		// blush pink bg, deep crimson text
		"pending": {rgb{0xff, 0xd8, 0xd8}, rgb{0x88, 0x20, 0x20}},
		// warm salmon bg, dark blood red text
		"overdue": {rgb{0xff, 0xb0, 0xb0}, rgb{0x88, 0x00, 0x00}},
		// spring meadow bg, deep forest text
		"reviewed": {rgb{0xd0, 0xf0, 0xd0}, rgb{0x15, 0x55, 0x20}},
		// light silver bg, slate text
		"closed": {rgb{0xd8, 0xd8, 0xd8}, rgb{0x44, 0x44, 0x44}},
		// pale grey bg, mid-grey text
		"stale": {rgb{0xc0, 0xc0, 0xc0}, rgb{0x55, 0x55, 0x55}},
	},
	GradientStart:   rgb{140, 120, 255}, // soft periwinkle
	GradientFgStart: rgb{0, 0, 0},       // pure black

	SubRowBgAnchor: rgb{0xf0, 0xf0, 0xf0}, // near-white
	SubRowFgAnchor: rgb{0x90, 0x90, 0x90}, // cool grey
	SubRowBgBlend:  0.4,
	SubRowFgBlend:  0.25,

	HeaderFg:    "0",   // black
	SeparatorFg: "245", // mid-grey

	HelpBrightFg: "236",     // dark grey (keys)
	HelpFg:       "#6c7086", // darker blue-grey (descriptions)
	HelpSepFg:    "247",     // neutral mid-grey (separators, brackets)
	HelpDimFg:    "250",     // very light grey (inactive pane)

	StatusFg: "166", // burnt orange

	NormalOptionFg:      "233", // near-black
	HighlightedOptionBg: "57",  // deep indigo
	HighlightedOptionFg: "15",  // bright white
	OptionNumFg:         "240", // mid-grey

	AfrtFg: "98", // medium lavender

	ChecksPassFg:    "28",  // dark green
	ChecksFailFg:    "124", // dark red
	ChecksWarnFg:    "166", // burnt orange
	ChecksZeroFg:    "245", // mid-grey
	ChecksPendingFg: "242", // cool grey (mbox view: incomplete checks)

	MboxHeaderLabelFg: "0",   // black
	MboxHeaderValueFg: "238", // dark grey
	DiffAddFg:         "28",  // dark green
	DiffDelFg:         "124", // dark red
	DiffHunkFg:        "30",  // dark teal
	QuotedLineFg:      "125", // muted plum
	WrapIndicatorFg:   "245", // mid-grey

	LogLineFg:  "242",     // cool grey
	ApplyLogFg: "#4c4f69", // catppuccin Text — dark, clearly readable
}
