package main

import (
	"math"
	"math/rand"
	"sync"
)

// State is what the cat is doing right now. Every state maps to a sprite set in
// sprites.go and to a block of movement logic in Step.
type State int

const (
	Idle          State = iota // sits still, tail flicking
	Wander                     // walks to a random spot it picked
	Follow                     // walks toward the cursor, then sits next to it
	FallingAsleep              // lie-down transition, played once
	Asleep                     // curled up with Z's; ignores the cursor
	WakingUp                   // the lie-down transition in reverse
	Angry                      // X eyes, right before bolting away
	BeingPetted                // blushing, held while the pointer is on the cat

	numStates
)

// String makes states readable in debug logs.
func (s State) String() string {
	switch s {
	case Idle:
		return "idle"
	case Wander:
		return "wander"
	case Follow:
		return "follow"
	case FallingAsleep:
		return "falling-asleep"
	case Asleep:
		return "asleep"
	case WakingUp:
		return "waking-up"
	case Angry:
		return "angry"
	case BeingPetted:
		return "being-petted"
	}
	return "unknown"
}

// asleepState reports whether the cursor should be ignored in this state.
func (s State) asleepState() bool {
	return s == FallingAsleep || s == Asleep || s == WakingUp
}

// View is the render-side snapshot of the cat: everything the window needs to
// draw a frame, copied out under the lock so the UI never touches live state.
type View struct {
	State  State
	Frame  int
	Facing int // +1 right, -1 left
	Moving bool
	X, Y   float32
}

// Cat is the simulation. It owns no UI: Step advances it by one tick and View
// reports what to draw, which keeps the whole behaviour testable without a
// display.
type Cat struct {
	mu  sync.Mutex
	cfg Config
	rng *rand.Rand

	x, y   float32 // top-left of the window, in screen points
	tx, ty float32 // where it is walking to
	speed  float64
	facing int
	moving bool

	state    State
	frame    int
	ticks    int // ticks spent in the current state
	stateFor int // ticks to stay in the current state

	// Sleep schedule: ticks of wakefulness left before the next nap. The nap's
	// own length lives in stateFor while the cat is Asleep.
	awakeLeft int

	// Following
	followEnabled bool
	wantsFollow   bool  // rerolled whenever it settles, so it is not always clingy
	cursor        Point // last known cursor position
	cursorOK      bool
	cursorStill   int // ticks since the cursor last moved

	// Petting
	pets       int
	lastPetAgo int

	frameCounts [numStates]int

	catW, catH       float32
	screenW, screenH float32
}

// NewCat builds a cat centred on the screen. frameCounts is how many sprites
// each state animates through.
func NewCat(cfg Config, screenW, screenH, catW, catH float32, frameCounts [numStates]int, rng *rand.Rand) *Cat {
	c := &Cat{
		cfg:           cfg,
		rng:           rng,
		facing:        1,
		speed:         cfg.WalkSpeed,
		followEnabled: true,
		frameCounts:   frameCounts,
		catW:          catW,
		catH:          catH,
		screenW:       screenW,
		screenH:       screenH,
	}
	c.x = (screenW - catW) / 2
	c.y = (screenH - catH) / 2
	c.awakeLeft = c.randTicks(cfg.AwakeMinSec, cfg.AwakeMaxSec)
	c.enter(Idle)
	return c
}

// Resize tells the cat the screen changed size, e.g. the display was switched
// or the app moved to a different monitor.
func (c *Cat) Resize(screenW, screenH float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.screenW, c.screenH = screenW, screenH
	c.clamp()
}

// View copies out the current frame.
func (c *Cat) View() View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return View{
		State:  c.state,
		Frame:  c.frame,
		Facing: c.facing,
		Moving: c.moving,
		X:      c.x,
		Y:      c.y,
	}
}

// ---------------------------------------------------------------- state machine

// enter switches state and picks how long to stay there. Caller holds the lock.
func (c *Cat) enter(s State) {
	c.state = s
	c.frame = 0
	c.ticks = 0
	c.moving = false

	switch s {
	case Idle:
		c.stateFor = c.randTicks(2, 6)
		// Reroll interest so the cat is not permanently glued to the cursor.
		c.wantsFollow = c.rng.Intn(100) < c.cfg.FollowChance
	case Wander:
		c.stateFor = c.cfg.ticks(60) // safety cap; normally ends on arrival
	case Follow:
		c.stateFor = c.cfg.ticks(c.cfg.FollowMaxSec)
	case FallingAsleep:
		c.stateFor = c.frameCounts[FallingAsleep] * framesEvery(FallingAsleep)
	case Asleep:
		c.stateFor = c.randTicks(c.cfg.SleepMinSec, c.cfg.SleepMaxSec)
	case WakingUp:
		c.stateFor = c.frameCounts[WakingUp] * framesEvery(WakingUp)
	case Angry:
		c.stateFor = c.cfg.ticks(2)
	case BeingPetted:
		c.stateFor = math.MaxInt32 // until the pointer leaves
	}
}

// Step advances the simulation by one tick. cursor is the global pointer
// position; ok is false when the platform cannot report it, in which case the
// cat simply never follows.
func (c *Cat) Step(cursor Point, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ticks++
	c.trackCursor(cursor, ok)
	c.decayPets()
	c.updateSchedule()
	c.move()
	c.advance()
	c.animate()
}

// trackCursor records the pointer and how long it has been parked. Caller holds
// the lock.
func (c *Cat) trackCursor(cursor Point, ok bool) {
	if !ok {
		c.cursorOK = false
		return
	}
	const moved = 2 // points; ignores jitter from sub-pixel reporting
	if !c.cursorOK || math.Abs(float64(cursor.X-c.cursor.X)) > moved ||
		math.Abs(float64(cursor.Y-c.cursor.Y)) > moved {
		c.cursorStill = 0
	} else {
		c.cursorStill++
	}
	c.cursor = cursor
	c.cursorOK = true
}

// decayPets forgets attention the cat received a while ago. Caller holds the lock.
func (c *Cat) decayPets() {
	c.lastPetAgo++
	if c.pets > 0 && c.lastPetAgo > c.cfg.ticks(c.cfg.PetPatienceSec) {
		c.pets--
		c.lastPetAgo = 0
	}
}

// updateSchedule runs the awake/asleep clock. The cat is awake for a stretch,
// then takes a nap during which the cursor is ignored entirely. Caller holds
// the lock.
func (c *Cat) updateSchedule() {
	if c.state.asleepState() {
		return
	}

	if c.awakeLeft > 0 {
		c.awakeLeft--
		return
	}
	// Only settle down from a calm state: a fleeing or freshly petted cat
	// finishes what it is doing first.
	if c.state == Idle || c.state == Wander || c.state == Follow {
		c.enter(FallingAsleep)
	}
}

// move applies this tick's motion. Caller holds the lock.
func (c *Cat) move() {
	switch c.state {
	case Wander:
		if c.stepToward(c.tx, c.ty, c.speed) {
			c.enter(Idle)
		}
	case Follow:
		// Aim at a point one standoff short of the cursor so the cat sits
		// beside the pointer instead of underneath it.
		dx, dy := c.cursorDelta()
		dist := math.Hypot(float64(dx), float64(dy))
		if dist <= c.cfg.FollowStandoff {
			c.moving = false
			c.faceTowards(dx)
			return
		}
		scale := (dist - c.cfg.FollowStandoff) / dist
		c.stepToward(c.x+float32(float64(dx)*scale), c.y+float32(float64(dy)*scale), c.cfg.FollowSpeed)
	default:
		c.moving = false
	}
}

// stepToward moves at most speed points towards (tx, ty) and reports arrival.
// Caller holds the lock.
func (c *Cat) stepToward(tx, ty float32, speed float64) bool {
	dx, dy := tx-c.x, ty-c.y
	dist := math.Hypot(float64(dx), float64(dy))
	if dist <= speed {
		c.x, c.y = tx, ty
		c.moving = false
		c.clamp()
		return true
	}
	c.x += float32(float64(dx) / dist * speed)
	c.y += float32(float64(dy) / dist * speed)
	c.moving = true
	c.faceTowards(dx)
	c.clamp()
	return false
}

func (c *Cat) faceTowards(dx float32) {
	if dx > 0.5 {
		c.facing = 1
	} else if dx < -0.5 {
		c.facing = -1
	}
}

// cursorDelta is the vector from the cat's centre to the cursor. Caller holds
// the lock.
func (c *Cat) cursorDelta() (float32, float32) {
	return c.cursor.X - (c.x + c.catW/2), c.cursor.Y - (c.y + c.catH/2)
}

// cursorDist is how far the cursor is from the cat's centre, or +Inf when the
// position is unknown. Caller holds the lock.
func (c *Cat) cursorDist() float64 {
	if !c.cursorOK {
		return math.Inf(1)
	}
	dx, dy := c.cursorDelta()
	return math.Hypot(float64(dx), float64(dy))
}

// cursorInteresting reports whether the cursor is worth chasing: known, awake
// hours, following turned on, and moved recently. Caller holds the lock.
func (c *Cat) cursorInteresting() bool {
	return c.followEnabled && c.cursorOK &&
		c.cursorStill < c.cfg.ticks(c.cfg.CursorStillSec)
}

// advance runs the state transitions for this tick. Caller holds the lock.
func (c *Cat) advance() {
	switch c.state {
	case Idle:
		if c.wantsFollow && c.cursorInteresting() && c.cursorDist() < c.cfg.FollowRadius {
			c.enter(Follow)
			return
		}
	case Follow:
		// Give up when the cursor runs off, goes quiet while out of reach, or
		// the cat has simply been at it too long.
		if !c.followEnabled || c.cursorDist() > c.cfg.GiveUpRadius ||
			(!c.cursorInteresting() && c.cursorDist() > c.cfg.FollowStandoff) {
			c.enter(Idle)
			c.wantsFollow = false
			return
		}
	case Angry:
		if c.ticks == c.stateFor-1 {
			// The angry frame is done: bolt to the spot picked in Touch.
			c.speed = c.cfg.FleeSpeed
			c.enter(Wander)
			return
		}
	}

	if c.ticks < c.stateFor {
		return
	}

	switch c.state {
	case Idle:
		if c.rng.Intn(100) < 55 {
			c.pickTarget()
		} else {
			c.enter(Idle)
		}
	case FallingAsleep:
		c.enter(Asleep)
	case Asleep:
		c.enter(WakingUp)
	case WakingUp:
		c.awakeLeft = c.randTicks(c.cfg.AwakeMinSec, c.cfg.AwakeMaxSec)
		c.enter(Idle)
	case Follow, Wander:
		c.wantsFollow = false // bored of the cursor for a while
		c.enter(Idle)
	}
}

// animate advances the sprite frame. One-shot states hold their last frame
// rather than looping. Caller holds the lock.
func (c *Cat) animate() {
	every := framesEvery(c.state)
	if c.ticks%every != 0 {
		return
	}
	n := c.frameCounts[c.state]
	if n <= 0 {
		return
	}
	if c.state == FallingAsleep || c.state == WakingUp {
		if c.frame < n-1 {
			c.frame++
		}
		return
	}
	c.frame = (c.frame + 1) % n
}

// framesEvery is how many ticks one animation frame lasts in a given state.
func framesEvery(s State) int {
	switch s {
	case Asleep:
		return 6
	case FallingAsleep, WakingUp:
		return 4
	case BeingPetted:
		return 2
	default:
		return 1
	}
}

// pickTarget aims at a random spot on the screen and starts walking. Caller
// holds the lock.
func (c *Cat) pickTarget() {
	c.tx = c.rng.Float32() * (c.screenW - c.catW)
	c.ty = c.rng.Float32() * (c.screenH - c.catH)
	c.speed = c.cfg.WalkSpeed
	c.enter(Wander)
}

func (c *Cat) clamp() {
	c.x = clampF(c.x, 0, max(c.screenW-c.catW, 0))
	c.y = clampF(c.y, 0, max(c.screenH-c.catH, 0))
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

// randTicks returns a random duration in [lo, hi] seconds, as ticks.
func (c *Cat) randTicks(loSec, hiSec int) int {
	if hiSec <= loSec {
		return c.cfg.ticks(loSec)
	}
	return c.cfg.ticks(loSec + c.rng.Intn(hiSec-loSec+1))
}

// ---------------------------------------------------------------- interaction

// Touch is one unit of attention: a tap, or the pointer stroking across the
// cat. fromX is where on the sprite it landed, which decides which way a
// startled cat runs.
func (c *Cat) Touch(fromX float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	woken := c.state.asleepState()
	c.pets++
	c.lastPetAgo = 0

	if woken {
		// Being woken up resets the nap, whether or not it ends in a huff.
		c.awakeLeft = c.randTicks(c.cfg.AwakeMinSec, c.cfg.AwakeMaxSec)
	}

	// An already cross cat does not calm down because you kept touching it.
	if c.state == Angry {
		c.startle(fromX)
		return
	}

	// A rudely woken cat, or one that has had enough, gets angry and bolts.
	if (woken && c.rng.Intn(2) == 0) || c.pets > c.cfg.PetsBeforeAnnoyed {
		c.pets = 0
		c.startle(fromX)
		return
	}

	if woken {
		c.enter(WakingUp)
		return
	}
	if c.state != BeingPetted {
		c.enter(BeingPetted)
	}
}

// startle sets an escape route away from the hand and shows the angry frame.
// Caller holds the lock.
func (c *Cat) startle(fromX float32) {
	c.enter(Angry)
	if fromX > c.catW/2 {
		c.tx = 0 // the hand came from the right, so run left
		c.facing = -1
	} else {
		c.tx = c.screenW - c.catW
		c.facing = 1
	}
	c.ty = c.rng.Float32() * (c.screenH - c.catH)
}

// HoverEnd is called when the pointer leaves the cat.
func (c *Cat) HoverEnd() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == BeingPetted {
		c.enter(Idle)
	}
}

// SetFollow turns cursor following on or off.
func (c *Cat) SetFollow(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.followEnabled = on
	if !on && c.state == Follow {
		c.enter(Idle)
	}
}

// Following reports whether cursor following is on.
func (c *Cat) Following() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.followEnabled
}

// Sleeping reports whether the cat is napping.
func (c *Cat) Sleeping() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.asleepState()
}

// Nap puts the cat to sleep now, ending the current awake stretch.
func (c *Cat) Nap() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.awakeLeft = 0
	if !c.state.asleepState() {
		c.enter(FallingAsleep)
	}
}

// Wake ends a nap early and starts a fresh awake stretch.
func (c *Cat) Wake() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.awakeLeft = c.randTicks(c.cfg.AwakeMinSec, c.cfg.AwakeMaxSec)
	if c.state.asleepState() {
		c.enter(WakingUp)
	}
}
