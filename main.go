// Desktop kitty: a borderless always-on-top cat that wanders the whole screen
// and reacts to being petted.
//
// Sprite map (asset file names are missing their first letter on disk, that is
// how they were downloaded -- kept as-is so nothing has to be renamed):
//
//	assets/idle1..4.png        -> idle loop: cat sits, tail flicks
//	assets/dle1..4.png         -> byte-identical copies of idle1..4, unused
//	assets/alkingright1..4.png -> "walkingright": walk cycle facing right
//	assets/alkingleft1..4.png  -> "walkingleft":  walk cycle facing left
//	assets/leeping1..6.png     -> "sleeping": lie-down transition, played once
//	assets/zz1..4.png          -> curled-up deep sleep loop with Z's
//	assets/ngry.png            -> "angry": X eyes, shown when over-petted or woken
//	assets/blush1..4.png       -> idle1..4 with pink cheeks and happy eyes, shown
//	                              while the cat is being petted. Generated from
//	                              the idle frames, see the blush sprite tool.
package main

import (
	"embed"
	"image/color"
	"math"
	"math/rand"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-gl/glfw/v3.4/glfw"
)

//go:embed assets/*.png
var assets embed.FS

const (
	catW = 72 // sprite size, also the window size
	catH = 64

	tick = 100 * time.Millisecond

	walkSpeed = 3.0 // points per tick when wandering
	fleeSpeed = 9.0 // points per tick when running away after being annoyed

	petsBeforeAnnoyed = 6  // taps/strokes tolerated before the cat gets angry
	petPatienceTicks  = 60 // pet counter decays if left alone this long
)

type State int

const (
	Idle State = iota
	Walking
	FallingAsleep // leeping1..6, one shot
	Asleep        // zz1..4, loop
	Angry         // ngry.png
	BeingPetted   // idle frames, held while the mouse is on the cat
)

type Cat struct {
	mu sync.Mutex

	x, y   float32 // window position on screen, in points
	tx, ty float32 // where the cat is walking to
	speed  float32
	facing int // +1 right, -1 left

	state      State
	frame      int
	ticks      int // ticks spent in the current state
	stateFor   int // how long to stay in the current state
	pets       int
	lastPetAgo int

	frames map[State][]fyne.Resource
	walkR  []fyne.Resource
	walkL  []fyne.Resource

	screenW, screenH float32
}

// ---------------------------------------------------------------- assets

func mustLoad(names ...string) []fyne.Resource {
	out := make([]fyne.Resource, 0, len(names))
	for _, n := range names {
		data, err := assets.ReadFile("assets/" + n)
		if err != nil {
			panic(err)
		}
		out = append(out, fyne.NewStaticResource(n, data))
	}
	return out
}

func newCat(screenW, screenH float32) *Cat {
	c := &Cat{
		x: screenW / 2, y: screenH / 2,
		facing: 1, speed: walkSpeed,
		screenW: screenW, screenH: screenH,
		frames: map[State][]fyne.Resource{
			Idle:          mustLoad("idle1.png", "idle2.png", "idle3.png", "idle4.png"),
			BeingPetted:   mustLoad("blush1.png", "blush2.png", "blush3.png", "blush4.png"),
			FallingAsleep: mustLoad("leeping1.png", "leeping2.png", "leeping3.png", "leeping4.png", "leeping5.png", "leeping6.png"),
			Asleep:        mustLoad("zz1.png", "zz2.png", "zz3.png", "zz4.png"),
			Angry:         mustLoad("ngry.png"),
		},
		walkR: mustLoad("alkingright1.png", "alkingright2.png", "alkingright3.png", "alkingright4.png"),
		walkL: mustLoad("alkingleft1.png", "alkingleft2.png", "alkingleft3.png", "alkingleft4.png"),
	}
	c.frames[Walking] = c.walkR
	c.enter(Idle)
	return c
}

// ---------------------------------------------------------------- state machine

// enter switches state and picks how long to stay there. Caller holds the lock.
func (c *Cat) enter(s State) {
	c.state = s
	c.frame = 0
	c.ticks = 0

	switch s {
	case Idle:
		c.stateFor = 20 + rand.Intn(40) // 2s - 6s
	case Walking:
		c.stateFor = 600 // safety cap, normally ends on arrival
	case FallingAsleep:
		c.stateFor = len(c.frames[FallingAsleep]) * 4
	case Asleep:
		c.stateFor = 200 + rand.Intn(400) // 20s - 60s
	case Angry:
		c.stateFor = 15
	case BeingPetted:
		c.stateFor = 1 << 30 // until the mouse leaves
	}
}

// pickTarget aims at a random spot anywhere on the screen. Caller holds the lock.
func (c *Cat) pickTarget() {
	c.tx = rand.Float32() * (c.screenW - catW)
	c.ty = rand.Float32() * (c.screenH - catH)
	c.speed = walkSpeed
	c.enter(Walking)
}

// framesEvery is how many ticks one animation frame lasts per state.
func framesEvery(s State) int {
	switch s {
	case Asleep:
		return 6
	case FallingAsleep:
		return 4
	case BeingPetted:
		return 2
	default:
		return 1
	}
}

// step advances one tick and reports whether the window has to move.
func (c *Cat) step() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ticks++
	c.lastPetAgo++
	if c.lastPetAgo > petPatienceTicks && c.pets > 0 {
		c.pets--
		c.lastPetAgo = 0
	}

	if c.state == Walking {
		dx, dy := c.tx-c.x, c.ty-c.y
		dist := float32(math.Hypot(float64(dx), float64(dy)))
		if dist < c.speed {
			c.x, c.y = c.tx, c.ty
			c.enter(Idle)
		} else {
			c.x += dx / dist * c.speed
			c.y += dy / dist * c.speed
			if dx > 0.5 {
				c.facing = 1
			} else if dx < -0.5 {
				c.facing = -1
			}
			c.clamp()
		}
	}

	if c.ticks >= c.stateFor {
		switch c.state {
		case Idle:
			switch n := rand.Intn(100); {
			case n < 65:
				c.pickTarget()
			case n < 85:
				c.enter(Idle)
			default:
				c.enter(FallingAsleep)
			}
		case FallingAsleep:
			c.enter(Asleep)
		case Asleep, Angry, Walking:
			c.enter(Idle)
		}
	}

	if c.ticks%framesEvery(c.state) == 0 {
		set := c.currentFrames()
		if c.state == FallingAsleep {
			// one shot: hold on the last frame instead of looping
			if c.frame < len(set)-1 {
				c.frame++
			}
		} else {
			c.frame = (c.frame + 1) % len(set)
		}
	}
}

// currentFrames returns the sprite set for the current state. Caller holds the lock.
func (c *Cat) currentFrames() []fyne.Resource {
	if c.state == Walking {
		if c.facing < 0 {
			return c.walkL
		}
		return c.walkR
	}
	return c.frames[c.state]
}

func (c *Cat) clamp() {
	c.x = clampF(c.x, 0, c.screenW-catW)
	c.y = clampF(c.y, 0, c.screenH-catH)
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------------------------------------------------------------- petting

// touch is one unit of attention: a tap, or the mouse moving across the cat.
// fromX is where on the sprite it happened, used to pick a flee direction.
func (c *Cat) touch(fromX float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	woken := c.state == Asleep || c.state == FallingAsleep
	c.pets++
	c.lastPetAgo = 0

	// Woken cats and over-petted cats get angry and then bolt away.
	if (woken && rand.Intn(2) == 0) || c.pets > petsBeforeAnnoyed {
		c.pets = 0
		c.enter(Angry)
		if fromX > catW/2 {
			c.tx = 0 // hand came from the right, run left
			c.facing = -1
		} else {
			c.tx = c.screenW - catW
			c.facing = 1
		}
		c.ty = rand.Float32() * (c.screenH - catH)
		return
	}

	if c.state != BeingPetted {
		c.enter(BeingPetted)
	}
}

// hoverEnd is called when the pointer leaves the cat.
func (c *Cat) hoverEnd() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == BeingPetted {
		c.enter(Idle)
	}
}

// afterAngry lets the cat sprint off once the angry frame is done.
func (c *Cat) fleeIfAngryDone() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Angry && c.ticks == c.stateFor-1 {
		c.speed = fleeSpeed
		c.enter(Walking)
	}
}

// ---------------------------------------------------------------- widget

// catWidget is the clickable/hoverable surface holding the sprite.
type catWidget struct {
	widget.BaseWidget
	img      *canvas.Image
	cat      *Cat
	strokeAt float32 // x of the last counted stroke
}

func newCatWidget(c *Cat) *catWidget {
	img := canvas.NewImageFromResource(c.frames[Idle][0])
	img.FillMode = canvas.ImageFillContain
	img.ScaleMode = canvas.ImageScalePixels // pixel art, no blurring
	img.SetMinSize(fyne.NewSize(catW, catH))

	w := &catWidget{img: img, cat: c}
	w.ExtendBaseWidget(w)
	return w
}

func (w *catWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(w.img)
}

func (w *catWidget) Tapped(e *fyne.PointEvent)        { w.cat.touch(e.Position.X) }
func (w *catWidget) TappedSecondary(*fyne.PointEvent) { w.cat.touch(catW / 2) }
func (w *catWidget) MouseIn(e *desktop.MouseEvent)    { w.cat.touch(e.Position.X) }
func (w *catWidget) MouseOut()                        { w.cat.hoverEnd() }
func (w *catWidget) Cursor() desktop.Cursor           { return desktop.PointerCursor }

// MouseMoved counts a stroke every few points of travel, so slowly stroking the
// cat is petting but resting the pointer on it is not.
func (w *catWidget) MouseMoved(e *desktop.MouseEvent) {
	if math.Abs(float64(e.Position.X-w.strokeAt)) > 12 {
		w.strokeAt = e.Position.X
		w.cat.touch(e.Position.X)
	}
}

// ---------------------------------------------------------------- screen size

// screenSize asks the OS how big the primary display is, in points. Fyne has no
// API for this, so it is one small platform query with a sane fallback.
func screenSize() (float32, float32) {
	var out []byte
	var err error

	switch runtime.GOOS {
	case "darwin":
		out, err = exec.Command("osascript", "-l", "JavaScript", "-e",
			`ObjC.import("AppKit"); var f=$.NSScreen.mainScreen.frame; f.size.width+"x"+f.size.height`).Output()
	case "linux":
		out, err = exec.Command("sh", "-c",
			`xrandr | awk '/\*/ {print $1; exit}'`).Output()
	case "windows":
		out, err = exec.Command("powershell", "-NoProfile", "-Command",
			`Add-Type -AssemblyName System.Windows.Forms; $b=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds; "$($b.Width)x$($b.Height)"`).Output()
	}
	if err == nil {
		s := strings.TrimSpace(string(out))
		if i := strings.IndexAny(s, "x*"); i > 0 {
			wv, e1 := strconv.ParseFloat(strings.TrimSpace(s[:i]), 32)
			hv, e2 := strconv.ParseFloat(strings.TrimSpace(s[i+1:]), 32)
			if e1 == nil && e2 == nil && wv > 0 && hv > 0 {
				return float32(wv), float32(hv)
			}
		}
	}
	return 1920, 1080
}

// ---------------------------------------------------------------- transparency

// clearTheme is the default theme with a fully transparent window background.
//
// Fyne has no transparent-window API. The window itself is already borderless
// (CreateSplashWindow), but the canvas still clears to
// theme.ColorNameBackground, which paints an opaque box behind the sprite.
// Returning a zero-alpha colour here makes the GL clear colour transparent;
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
// window. It must run after GLFW is initialised and before the window is
// created: creating the splash window initialises GLFW but does not open it
// (that happens on Show), and Fyne never resets the window hints in between, so
// this is the window in which the hint has to be set. Must be the main
// goroutine, which is where main() runs.
func makeWindowTransparent() {
	glfw.WindowHint(glfw.TransparentFramebuffer, glfw.True)
}

// ---------------------------------------------------------------- main

func main() {
	// All UI work below goes through fyne.Do, so build with
	//   go build -tags migrated_fynedo
	// to silence Fyne's threading-migration notice.
	a := app.NewWithID("com.mohakgupta.desktopkitty")
	a.Settings().SetTheme(clearTheme{theme.DefaultTheme()})

	sw, sh := screenSize()
	cat := newCat(sw, sh)

	var w fyne.Window
	if drv, ok := a.Driver().(desktop.Driver); ok {
		w = drv.CreateSplashWindow() // borderless, no title bar
		makeWindowTransparent()      // after GLFW init, before the window opens
	} else {
		w = a.NewWindow("kitty")
	}
	if dw, ok := w.(desktop.Window); ok {
		dw.RequestAlwaysOnTop()
		dw.RequestPosition(int(cat.x), int(cat.y))
	}

	cw := newCatWidget(cat)
	w.SetPadded(false)
	w.SetContent(cw)
	w.Resize(fyne.NewSize(catW, catH))
	w.SetFixedSize(true)

	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for range t.C {
			cat.fleeIfAngryDone()
			cat.step()

			cat.mu.Lock()
			res := cat.currentFrames()[cat.frame]
			x, y := int(cat.x), int(cat.y)
			cat.mu.Unlock()

			fyne.Do(func() { // all UI work must happen on the main thread
				if cw.img.Resource != res {
					cw.img.Resource = res
					cw.img.Refresh()
				}
				if dw, ok := w.(desktop.Window); ok {
					dw.RequestPosition(x, y)
				}
			})
		}
	}()

	w.ShowAndRun()
}
