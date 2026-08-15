package main

import (
	"math"
	"math/rand"
	"testing"
)

// testFrameCounts matches the shipped sprite sheet.
var testFrameCounts = [numStates]int{
	Idle: 4, Wander: 4, Follow: 4,
	FallingAsleep: 6, Asleep: 4, WakingUp: 6,
	Angry: 1, BeingPetted: 4,
}

// newTestCat builds a cat with a fixed seed so every test is deterministic.
// mutate can adjust the config before the cat is created.
func newTestCat(t *testing.T, mutate func(*Config)) *Cat {
	t.Helper()

	cfg := DefaultConfig()
	cfg.FollowChance = 100 // remove the "does it feel like it" coin flip
	if mutate != nil {
		mutate(&cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config is invalid: %v", err)
	}
	return NewCat(cfg, 1000, 800, catW, catH, testFrameCounts, rand.New(rand.NewSource(1)))
}

// stepFor advances n ticks with the cursor jittering by a couple of points each
// tick, which is what a real moving cursor looks like to the cat.
func stepFor(c *Cat, n int, cursor Point, ok bool) {
	for i := 0; i < n; i++ {
		p := cursor
		if i%2 == 0 {
			p.X += 4 // keep the cursor "active"
		}
		c.Step(p, ok)
	}
}

func TestFollowsAMovingCursor(t *testing.T) {
	c := newTestCat(t, nil)
	start := c.View()
	cursor := Point{X: start.X + 300, Y: start.Y}

	stepFor(c, 40, cursor, true)

	if got := c.View(); got.X <= start.X {
		t.Fatalf("cat did not move toward the cursor: started at x=%v, now x=%v", start.X, got.X)
	}
	if got := c.View().State; got != Follow {
		t.Fatalf("state = %v, want follow", got)
	}
}

func TestFollowStopsAtStandoff(t *testing.T) {
	c := newTestCat(t, nil)
	start := c.View()
	cursor := Point{X: start.X + 400, Y: start.Y + 200}

	stepFor(c, 400, cursor, true)

	c.mu.Lock()
	dist := math.Hypot(float64(cursor.X-(c.x+c.catW/2)), float64(cursor.Y-(c.y+c.catH/2)))
	c.mu.Unlock()

	// The cursor jitters by 4 points, so allow one step of slack.
	if want := c.cfg.FollowStandoff + c.cfg.FollowSpeed + 4; dist > want {
		t.Fatalf("cat stopped %v away from the cursor, want at most %v", dist, want)
	}
	if v := c.View(); v.Moving {
		t.Fatal("cat should be sitting once it has reached the cursor")
	}
}

func TestIgnoresACursorItCannotSee(t *testing.T) {
	c := newTestCat(t, nil)
	start := c.View()

	// ok=false is what an unsupported platform reports.
	stepFor(c, 60, Point{X: start.X + 300, Y: start.Y}, false)

	if got := c.View().State; got == Follow {
		t.Fatal("cat followed a cursor whose position is unknown")
	}
}

func TestIgnoresAParkedCursor(t *testing.T) {
	c := newTestCat(t, nil)
	start := c.View()
	cursor := Point{X: start.X + 200, Y: start.Y}

	// No jitter: the cursor never moves, so it stops being interesting.
	for i := 0; i < 300; i++ {
		c.Step(cursor, true)
	}
	if got := c.View().State; got == Follow {
		t.Fatal("cat kept chasing a cursor that had not moved in minutes")
	}
}

func TestFallsAsleepAndIgnoresTheCursor(t *testing.T) {
	c := newTestCat(t, func(cfg *Config) {
		cfg.AwakeMinSec, cfg.AwakeMaxSec = 2, 2
		cfg.SleepMinSec, cfg.SleepMaxSec = 30, 30
	})

	cursor := Point{X: 900, Y: 700}
	stepFor(c, 60, cursor, true)

	if got := c.View().State; !got.asleepState() {
		t.Fatalf("state = %v, want the cat to be napping after its awake time ran out", got)
	}

	// While asleep the cursor must not move it at all.
	asleepAt := c.View()
	stepFor(c, 100, cursor, true)
	after := c.View()

	if after.X != asleepAt.X || after.Y != asleepAt.Y {
		t.Fatalf("sleeping cat moved from (%v,%v) to (%v,%v)", asleepAt.X, asleepAt.Y, after.X, after.Y)
	}
	if !after.State.asleepState() {
		t.Fatalf("state = %v, want the cat to still be asleep", after.State)
	}
}

func TestWakesUpAfterItsNap(t *testing.T) {
	c := newTestCat(t, func(cfg *Config) {
		cfg.AwakeMinSec, cfg.AwakeMaxSec = 1, 1
		cfg.SleepMinSec, cfg.SleepMaxSec = 3, 3
	})

	cursor := Point{X: 900, Y: 700}
	stepFor(c, 20, cursor, true) // asleep by now

	if !c.Sleeping() {
		t.Fatal("expected the cat to be asleep")
	}

	// The nap is 3 seconds and waking up takes a few more ticks, so it must be
	// up well inside a minute of ticks.
	for i := 0; i < 600 && c.Sleeping(); i++ {
		c.Step(cursor, true)
	}
	if c.Sleeping() {
		t.Fatal("cat never woke up from a 3 second nap")
	}
}

func TestNapAndWakeFromTheTray(t *testing.T) {
	c := newTestCat(t, nil)

	c.Nap()
	c.Step(Point{}, false)
	if !c.Sleeping() {
		t.Fatal("Nap did not put the cat to sleep")
	}

	c.Wake()
	if !c.Sleeping() {
		t.Fatal("Wake should play the waking-up animation, which still counts as asleep")
	}
	stepFor(c, 40, Point{}, false)
	if c.Sleeping() {
		t.Fatal("cat did not finish waking up")
	}
}

func TestSetFollowStopsAChase(t *testing.T) {
	c := newTestCat(t, nil)
	start := c.View()
	stepFor(c, 30, Point{X: start.X + 300, Y: start.Y}, true)

	if c.View().State != Follow {
		t.Fatal("expected the cat to be following before the toggle")
	}

	c.SetFollow(false)
	if c.View().State == Follow {
		t.Fatal("SetFollow(false) did not end the chase")
	}

	stepFor(c, 60, Point{X: start.X + 300, Y: start.Y}, true)
	if c.View().State == Follow {
		t.Fatal("cat started following again with following turned off")
	}
	if c.Following() {
		t.Fatal("Following() should report false")
	}
}

func TestPettingAndOverPetting(t *testing.T) {
	c := newTestCat(t, nil)

	c.Touch(catW / 2)
	if got := c.View().State; got != BeingPetted {
		t.Fatalf("state = %v, want being-petted", got)
	}

	c.HoverEnd()
	if got := c.View().State; got != Idle {
		t.Fatalf("state after the pointer left = %v, want idle", got)
	}

	// One touch past the limit should make it angry.
	for i := 0; i <= c.cfg.PetsBeforeAnnoyed; i++ {
		c.Touch(catW - 1) // petted from the right, so it runs left
	}
	if got := c.View().State; got != Angry {
		t.Fatalf("state = %v, want angry after %d pets", got, c.cfg.PetsBeforeAnnoyed+1)
	}

	c.mu.Lock()
	tx, facing := c.tx, c.facing
	c.mu.Unlock()
	if tx != 0 || facing != -1 {
		t.Fatalf("cat fled to x=%v facing %d, want it to run left to x=0", tx, facing)
	}

	// After the angry frame it bolts at flee speed.
	stepFor(c, c.cfg.ticks(2)+1, Point{}, false)
	c.mu.Lock()
	state, speed := c.state, c.speed
	c.mu.Unlock()
	if state != Wander || speed != c.cfg.FleeSpeed {
		t.Fatalf("state = %v at speed %v, want wander at flee speed %v", state, speed, c.cfg.FleeSpeed)
	}
}

func TestTouchWakesTheCatUp(t *testing.T) {
	c := newTestCat(t, func(cfg *Config) {
		cfg.AwakeMinSec, cfg.AwakeMaxSec = 1, 1
		cfg.SleepMinSec, cfg.SleepMaxSec = 300, 300
	})

	stepFor(c, 40, Point{}, false)
	if !c.Sleeping() {
		t.Fatal("expected the cat to be asleep")
	}

	c.Touch(catW / 2)
	if got := c.View().State; got != WakingUp && got != Angry {
		t.Fatalf("state = %v, want the cat to wake up or get cross", got)
	}

	// However it took the interruption, it owes itself a fresh awake stretch.
	c.mu.Lock()
	awake := c.awakeLeft
	c.mu.Unlock()
	if awake <= 0 {
		t.Fatal("waking the cat did not restart its awake time")
	}
}

func TestStaysOnScreen(t *testing.T) {
	c := newTestCat(t, nil)

	// A cursor far off-screen must never drag the cat past the edge.
	stepFor(c, 500, Point{X: 5000, Y: 5000}, true)

	v := c.View()
	if v.X < 0 || v.Y < 0 || v.X > 1000-catW || v.Y > 800-catH {
		t.Fatalf("cat left the screen at (%v,%v)", v.X, v.Y)
	}
}

func TestResizeKeepsTheCatInBounds(t *testing.T) {
	c := newTestCat(t, nil)
	stepFor(c, 200, Point{X: 990, Y: 790}, true)

	c.Resize(300, 200)
	v := c.View()
	if v.X > 300-catW || v.Y > 200-catH {
		t.Fatalf("cat at (%v,%v) is outside the smaller 300x200 screen", v.X, v.Y)
	}
}

func TestAnimationFrameStaysInRange(t *testing.T) {
	sprites, err := LoadSprites()
	if err != nil {
		t.Fatalf("LoadSprites: %v", err)
	}
	c := newTestCat(t, func(cfg *Config) {
		cfg.AwakeMinSec, cfg.AwakeMaxSec = 2, 3
		cfg.SleepMinSec, cfg.SleepMaxSec = 2, 3
	})

	// Run through many sleep/wake cycles and pet the cat along the way; every
	// frame has to resolve to a real sprite.
	for i := 0; i < 5000; i++ {
		c.Step(Point{X: float32(i % 900), Y: float32(i % 700)}, true)
		if i%97 == 0 {
			c.Touch(float32(i % catW))
		}
		v := c.View()
		if v.Frame < 0 || v.Frame >= sprites.counts[v.State] {
			t.Fatalf("frame %d out of range for state %v (%d frames)", v.Frame, v.State, sprites.counts[v.State])
		}
		if sprites.Frame(v) == nil {
			t.Fatalf("no sprite for %+v", v)
		}
	}
}

func TestStateStringsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for s := Idle; s < numStates; s++ {
		name := s.String()
		if name == "unknown" || seen[name] {
			t.Fatalf("state %d has a missing or duplicate name %q", s, name)
		}
		seen[name] = true
	}
}
