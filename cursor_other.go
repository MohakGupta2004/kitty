//go:build !darwin && !windows && !linux

package main

// cursorPos is unsupported on this platform, so the cat wanders instead of
// following.
func cursorPos() (Point, bool) { return Point{}, false }
