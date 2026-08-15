package main

// Cursor reporting.
//
// Fyne only delivers pointer events that land inside a window, so the cat can
// never see the cursor while it is somewhere else on the desktop. Following the
// cursor needs a global, screen-space position, which is a per-platform query:
//
//	cursor_darwin.go   Quartz CGEventGetLocation
//	cursor_windows.go  user32!GetCursorPos
//	cursor_linux.go    X11 XQueryPointer on the root window
//	cursor_other.go    everything else: unsupported
//
// Each file provides cursorPos. When it reports false the cat falls back to
// wandering on its own, which is also what happens on Wayland where no
// unprivileged global pointer query exists.

// Point is a position on the virtual desktop, in the same points/pixels unit
// that window positions use.
type Point struct {
	X, Y float32
}
