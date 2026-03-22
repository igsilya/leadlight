package tui

type theme struct {
	BgColors map[string]bgColor

	GradientStart   rgb
	GradientFgStart rgb

	HeaderFg, SeparatorFg           string
	HelpFg, HelpBrightFg, HelpDimFg string
	StatusFg                        string

	NormalOptionFg      string
	HighlightedOptionBg string
	HighlightedOptionFg string
	OptionNumFg         string

	ChecksPassFg, ChecksFailFg    string
	ChecksPendingFg, ChecksZeroFg string

	MboxHeaderLabelFg, MboxHeaderValueFg string
	DiffAddFg, DiffDelFg, DiffHunkFg     string
	QuotedLineFg, WrapIndicatorFg        string

	LogLineFg string
}

// Dark theme FG colors inspired by the Catppuccin Mocha palette.
// https://github.com/catppuccin/catppuccin (MIT license)
var darkTheme = theme{
	BgColors: map[string]bgColor{
		"aging":    {rgb{0x2a, 0x25, 0x20}, rgb{0xf9, 0xe2, 0xaf}},
		"active":   {rgb{0x2a, 0x2a, 0x30}, rgb{0xcd, 0xd6, 0xf4}},
		"pending":  {rgb{0x2a, 0x20, 0x30}, rgb{0xf3, 0x8b, 0xa8}},
		"overdue":  {rgb{0x2d, 0x18, 0x20}, rgb{0xf0, 0x58, 0x58}},
		"reviewed": {rgb{0x1a, 0x28, 0x20}, rgb{0xa6, 0xe3, 0xa1}},
		"closed":   {rgb{0x35, 0x35, 0x35}, rgb{0xcc, 0xcc, 0xcc}},
		"stale":    {rgb{0x12, 0x12, 0x12}, rgb{0x99, 0x99, 0x99}},
	},
	GradientStart:   rgb{95, 0, 255},
	GradientFgStart: rgb{255, 255, 255},

	HeaderFg: "15", SeparatorFg: "240",
	HelpFg: "241", HelpBrightFg: "250", HelpDimFg: "236",
	StatusFg: "214",

	NormalOptionFg:      "255",
	HighlightedOptionBg: "57",
	HighlightedOptionFg: "15",
	OptionNumFg:         "250",

	ChecksPassFg: "34", ChecksFailFg: "196",
	ChecksPendingFg: "214", ChecksZeroFg: "240",

	MboxHeaderLabelFg: "15", MboxHeaderValueFg: "252",
	DiffAddFg: "34", DiffDelFg: "196", DiffHunkFg: "6",
	QuotedLineFg: "168", WrapIndicatorFg: "242",

	LogLineFg: "245",
}

var lightTheme = theme{
	BgColors: map[string]bgColor{
		"aging":    {rgb{0xff, 0xf8, 0xd0}, rgb{0x55, 0x4d, 0x00}},
		"active":   {rgb{0xe8, 0xe8, 0xe8}, rgb{0x33, 0x33, 0x33}},
		"pending":  {rgb{0xff, 0xd8, 0xd8}, rgb{0x88, 0x20, 0x20}},
		"overdue":  {rgb{0xff, 0xb0, 0xb0}, rgb{0x88, 0x00, 0x00}},
		"reviewed": {rgb{0xd0, 0xf0, 0xd0}, rgb{0x15, 0x55, 0x20}},
		"closed":   {rgb{0xd8, 0xd8, 0xd8}, rgb{0x44, 0x44, 0x44}},
		"stale":    {rgb{0xc0, 0xc0, 0xc0}, rgb{0x55, 0x55, 0x55}},
	},
	GradientStart:   rgb{140, 120, 255},
	GradientFgStart: rgb{0, 0, 0},

	HeaderFg: "0", SeparatorFg: "245",
	HelpFg: "244", HelpBrightFg: "236", HelpDimFg: "250",
	StatusFg: "166",

	NormalOptionFg:      "233",
	HighlightedOptionBg: "57",
	HighlightedOptionFg: "15",
	OptionNumFg:         "240",

	ChecksPassFg: "28", ChecksFailFg: "124",
	ChecksPendingFg: "166", ChecksZeroFg: "245",

	MboxHeaderLabelFg: "0", MboxHeaderValueFg: "238",
	DiffAddFg: "28", DiffDelFg: "124", DiffHunkFg: "30",
	QuotedLineFg: "125", WrapIndicatorFg: "245",

	LogLineFg: "242",
}
