//go:build linux

package main

/*
#cgo LDFLAGS: -lX11
#include <stdlib.h>
#include <X11/Xlib.h>

// The display connection is opened once and reused: the cat asks for the
// cursor several times a second and a fresh XOpenDisplay per query would be a
// round trip to the X server plus a socket setup each time.
static Display *kittyDisplay = NULL;
static int kittyDisplayFailed = 0;

static int kittyCursor(int *x, int *y) {
	if (kittyDisplay == NULL) {
		if (kittyDisplayFailed) {
			return 0;
		}
		kittyDisplay = XOpenDisplay(NULL);
		if (kittyDisplay == NULL) {
			kittyDisplayFailed = 1; // no DISPLAY, or a pure Wayland session
			return 0;
		}
	}

	Window root = DefaultRootWindow(kittyDisplay);
	Window rootReturn, childReturn;
	int rootX, rootY, winX, winY;
	unsigned int mask;

	if (!XQueryPointer(kittyDisplay, root, &rootReturn, &childReturn,
			&rootX, &rootY, &winX, &winY, &mask)) {
		return 0; // pointer is on another screen
	}
	*x = rootX;
	*y = rootY;
	return 1;
}
*/
import "C"

func cursorPos() (Point, bool) {
	var x, y C.int
	if C.kittyCursor(&x, &y) == 0 {
		return Point{}, false
	}
	return Point{X: float32(x), Y: float32(y)}, true
}
