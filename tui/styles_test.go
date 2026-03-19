package tui

import "testing"

func TestRgbHex(t *testing.T) {
	tests := []struct {
		c    rgb
		want string
	}{
		{rgb{0, 0, 0}, "#000000"},
		{rgb{255, 255, 255}, "#ffffff"},
		{rgb{0x55, 0x4d, 0x00}, "#554d00"},
		{rgb{0x15, 0x50, 0x20}, "#155020"},
	}
	for _, tt := range tests {
		got := tt.c.hex()
		if got != tt.want {
			t.Errorf("rgb%v.hex() = %q, want %q",
				tt.c, got, tt.want)
		}
	}
}

func TestRgbLerp(t *testing.T) {
	black := rgb{0, 0, 0}
	white := rgb{255, 255, 255}

	if got := black.lerp(white, 0); got != black {
		t.Errorf("lerp(0) = %v, want %v", got, black)
	}
	if got := black.lerp(white, 1); got != white {
		t.Errorf("lerp(1) = %v, want %v", got, white)
	}

	mid := black.lerp(white, 0.5)
	if mid.r != 127 || mid.g != 127 || mid.b != 127 {
		t.Errorf("lerp(0.5) = %v, want ~{127,127,127}", mid)
	}

	lorem := rgb{100, 0, 200}
	dolor := rgb{200, 100, 0}
	q := lorem.lerp(dolor, 0.25)
	if q.r != 125 || q.g != 25 || q.b != 150 {
		t.Errorf("lerp(0.25) = %v, want {125,25,150}", q)
	}
}

func TestBuildStyles_BothThemes(t *testing.T) {
	for key := range darkTheme.BgColors {
		if _, ok := lightTheme.BgColors[key]; !ok {
			t.Errorf("light theme missing bgColor %q", key)
		}
	}
	for key := range lightTheme.BgColors {
		if _, ok := darkTheme.BgColors[key]; !ok {
			t.Errorf("dark theme missing bgColor %q", key)
		}
	}
}

func TestSetTheme(t *testing.T) {
	prev := activeTheme
	defer buildStyles(prev)

	SetTheme("dark")
	if activeTheme != &darkTheme {
		t.Error("expected dark theme")
	}
	SetTheme("light")
	if activeTheme != &lightTheme {
		t.Error("expected light theme")
	}
}
