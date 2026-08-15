//go:build darwin

package main

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

// kittyCursor writes the global cursor location, in points with the origin at
// the top-left of the main display -- the same coordinate space AppKit window
// positions use, so no flipping is needed.
static int kittyCursor(double *x, double *y) {
	CGEventRef ev = CGEventCreate(NULL);
	if (ev == NULL) {
		return 0;
	}
	CGPoint p = CGEventGetLocation(ev);
	CFRelease(ev);
	*x = p.x;
	*y = p.y;
	return 1;
}
*/
import "C"

func cursorPos() (Point, bool) {
	var x, y C.double
	if C.kittyCursor(&x, &y) == 0 {
		return Point{}, false
	}
	return Point{X: float32(x), Y: float32(y)}, true
}
