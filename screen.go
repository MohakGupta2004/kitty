package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Fallback size used when the display cannot be measured, chosen so the cat
// still stays somewhere on a typical screen.
const (
	fallbackScreenW = 1920
	fallbackScreenH = 1080
)

// screenSize asks the OS how big the primary display is. Fyne exposes no API
// for this, so it is one small platform query per OS with a sane fallback.
// The units match what each platform's window positions use: points on macOS,
// pixels elsewhere.
func screenSize(ctx context.Context) (float32, float32, error) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e",
			`ObjC.import("AppKit"); var f=$.NSScreen.mainScreen.frame; f.size.width+"x"+f.size.height`)
	case "linux":
		cmd = exec.CommandContext(ctx, "sh", "-c", `xrandr | awk '/\*/ {print $1; exit}'`)
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
			`Add-Type -AssemblyName System.Windows.Forms; `+
				`$b=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds; "$($b.Width)x$($b.Height)"`)
	default:
		return fallbackScreenW, fallbackScreenH, fmt.Errorf("no screen query for %s", runtime.GOOS)
	}

	out, err := cmd.Output()
	if err != nil {
		return fallbackScreenW, fallbackScreenH, fmt.Errorf("query screen size: %w", err)
	}

	w, h, err := parseScreenSize(string(out))
	if err != nil {
		return fallbackScreenW, fallbackScreenH, err
	}
	return w, h, nil
}

// parseScreenSize reads a "WIDTHxHEIGHT" pair, which is what every one of the
// platform commands above prints. Trailing junk is tolerated because xrandr
// marks the current mode with symbols, as in "1920x1080*+", and osascript
// prints the numbers as floats.
func parseScreenSize(s string) (float32, float32, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, "xX*")
	if i <= 0 {
		return 0, 0, fmt.Errorf("unrecognised screen size %q", s)
	}
	w, err1 := parseLeadingNumber(s[:i])
	h, err2 := parseLeadingNumber(s[i+1:])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("unrecognised screen size %q", s)
	}
	return float32(w), float32(h), nil
}

// parseLeadingNumber reads the number at the start of s and ignores whatever
// follows it.
func parseLeadingNumber(s string) (float64, error) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && (s[end] == '.' || (s[end] >= '0' && s[end] <= '9')) {
		end++
	}
	if end == 0 {
		return 0, fmt.Errorf("no number in %q", s)
	}
	return strconv.ParseFloat(s[:end], 32)
}

// watchScreenSize re-measures the display every interval and reports changes,
// so docking a laptop or switching monitors does not leave the cat stuck
// walking into what it thinks is the edge of the world.
func watchScreenSize(ctx context.Context, interval time.Duration, w, h float32, onChange func(w, h float32)) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			nw, nh, err := screenSize(ctx)
			if err != nil || (nw == w && nh == h) {
				continue
			}
			w, h = nw, nh
			onChange(w, h)
		}
	}
}
