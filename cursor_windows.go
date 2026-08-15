//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32.NewProc("GetCursorPos")
)

type winPoint struct {
	X, Y int32
}

func cursorPos() (Point, bool) {
	var p winPoint
	// GetCursorPos returns pixels in virtual-screen space, matching the size
	// reported by screenSize.
	r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		return Point{}, false
	}
	return Point{X: float32(p.X), Y: float32(p.Y)}, true
}
