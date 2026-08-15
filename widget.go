package main

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-gl/glfw/v3.4/glfw"
)

// strokeDistance is how far the pointer has to travel across the sprite, at
// scale 1, to count as one stroke. It stops a pointer that is merely resting on
// the cat from petting it forever.
const strokeDistance = 12

// catWidget is the hoverable, clickable surface that shows the sprite.
type catWidget struct {
	widget.BaseWidget

	img      *canvas.Image
	cat      *Cat
	scale    float32
	strokeAt float32 // where the last counted stroke happened
}

func newCatWidget(cat *Cat, sprites *Sprites, w, h float32) *catWidget {
	img := canvas.NewImageFromResource(sprites.Frame(cat.View()))
	img.FillMode = canvas.ImageFillContain
	img.ScaleMode = canvas.ImageScalePixels // pixel art: never blur it
	img.SetMinSize(fyne.NewSize(w, h))

	cw := &catWidget{img: img, cat: cat, scale: w / catW}
	cw.ExtendBaseWidget(cw)
	return cw
}

func (w *catWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(w.img)
}

// setFrame swaps the sprite, skipping the repaint when nothing changed. Must be
// called on the UI thread.
func (w *catWidget) setFrame(res fyne.Resource) {
	if w.img.Resource == res {
		return
	}
	w.img.Resource = res
	w.img.Refresh()
}

func (w *catWidget) Tapped(e *fyne.PointEvent)        { w.cat.Touch(e.Position.X) }
func (w *catWidget) TappedSecondary(*fyne.PointEvent) { w.cat.Touch(catW / 2) }
func (w *catWidget) MouseIn(e *desktop.MouseEvent)    { w.cat.Touch(e.Position.X) }
func (w *catWidget) MouseOut()                        { w.cat.HoverEnd() }
func (w *catWidget) Cursor() desktop.Cursor           { return desktop.PointerCursor }

// MouseMoved counts a stroke once the pointer has travelled far enough, so
// stroking the cat pets it but parking the pointer on it does not.
func (w *catWidget) MouseMoved(e *desktop.MouseEvent) {
	if math.Abs(float64(e.Position.X-w.strokeAt)) > float64(strokeDistance*w.scale) {
		w.strokeAt = e.Position.X
		w.cat.Touch(e.Position.X)
	}
}

// ---------------------------------------------------------------- transparency

// clearTheme is the default theme with a fully transparent window background.
//
// Fyne has no transparent-window API. The window itself is already borderless
// (CreateSplashWindow), but the canvas still clears to
// theme.ColorNameBackground, which paints an opaque box behind the sprite.
// Returning a zero-alpha colour makes the GL clear colour transparent;
// makeWindowTransparent below is the other half that lets that alpha through.
type clearTheme struct {
	fyne.Theme
}

func (clearTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameBackground {
		return color.Transparent
	}
	return theme.DefaultTheme().Color(name, variant)
}

// makeWindowTransparent asks GLFW for a framebuffer with a real alpha channel,
// which is what stops the compositor from drawing an opaque surface under the
// window. It has to run after GLFW is initialised and before the window is
// created: creating the splash window initialises GLFW but does not open it
// (that happens on Show), and Fyne never resets the window hints in between, so
// that is the gap this call has to land in. Main goroutine only, which is where
// run() is called from.
func makeWindowTransparent() {
	glfw.WindowHint(glfw.TransparentFramebuffer, glfw.True)
}
