package main

import "testing"

func TestParseScreenSize(t *testing.T) {
	ok := map[string][2]float32{
		"1920x1080":     {1920, 1080},
		"  1512x982\n":  {1512, 982},
		"1920x1080*+":   {1920, 1080}, // xrandr marks the active mode with a star
		"2560.0x1440.0": {2560, 1440}, // osascript prints floats
		"3840*2160":     {3840, 2160},
	}
	for in, want := range ok {
		w, h, err := parseScreenSize(in)
		if err != nil {
			t.Fatalf("parseScreenSize(%q): %v", in, err)
		}
		if w != want[0] || h != want[1] {
			t.Fatalf("parseScreenSize(%q) = %v, %v, want %v", in, w, h, want)
		}
	}

	for _, in := range []string{"", "nonsense", "x1080", "0x0", "-5x10", "1920x"} {
		if _, _, err := parseScreenSize(in); err == nil {
			t.Fatalf("parseScreenSize(%q) should have failed", in)
		}
	}
}
