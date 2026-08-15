package main

import (
	"embed"
	"fmt"

	"fyne.io/fyne/v2"
)

// Sprite map. The asset files are missing their first letter on disk -- that is
// how the sprite pack was downloaded, and renaming them would break nothing but
// is not worth the churn:
//
//	assets/idle1..4.png        idle loop: the cat sits, tail flicking
//	assets/alkingright1..4.png "walkingright": walk cycle facing right
//	assets/alkingleft1..4.png  "walkingleft":  walk cycle facing left
//	assets/leeping1..6.png     "sleeping": lie-down transition, played once
//	assets/zz1..4.png          curled-up deep sleep loop with Z's
//	assets/ngry.png            "angry": X eyes, shown when startled
//	assets/blush1..4.png       idle frames with pink cheeks, shown while petted
//	assets/dle1..4.png         byte-identical copies of idle1..4, unused
//
//go:embed assets/*.png
var assets embed.FS

// Sprite dimensions, which are also the window size at scale 1.
const (
	catW = 72
	catH = 64
)

// Sprites holds every animation frame, decoded once at start-up.
type Sprites struct {
	idle   []fyne.Resource
	blush  []fyne.Resource
	walkR  []fyne.Resource
	walkL  []fyne.Resource
	sleep  []fyne.Resource // lie-down transition
	zz     []fyne.Resource
	angry  []fyne.Resource
	counts [numStates]int
}

// LoadSprites reads the embedded sprite sheet. It returns an error instead of
// panicking so a corrupted build fails with a message a user can report.
func LoadSprites() (*Sprites, error) {
	load := func(names ...string) ([]fyne.Resource, error) {
		out := make([]fyne.Resource, 0, len(names))
		for _, n := range names {
			data, err := assets.ReadFile("assets/" + n)
			if err != nil {
				return nil, fmt.Errorf("load sprite %s: %w", n, err)
			}
			out = append(out, fyne.NewStaticResource(n, data))
		}
		return out, nil
	}

	var (
		s   Sprites
		err error
	)
	for _, step := range []struct {
		dst   *[]fyne.Resource
		names []string
	}{
		{&s.idle, []string{"idle1.png", "idle2.png", "idle3.png", "idle4.png"}},
		{&s.blush, []string{"blush1.png", "blush2.png", "blush3.png", "blush4.png"}},
		{&s.walkR, []string{"alkingright1.png", "alkingright2.png", "alkingright3.png", "alkingright4.png"}},
		{&s.walkL, []string{"alkingleft1.png", "alkingleft2.png", "alkingleft3.png", "alkingleft4.png"}},
		{&s.sleep, []string{"leeping1.png", "leeping2.png", "leeping3.png", "leeping4.png", "leeping5.png", "leeping6.png"}},
		{&s.zz, []string{"zz1.png", "zz2.png", "zz3.png", "zz4.png"}},
		{&s.angry, []string{"ngry.png"}},
	} {
		if *step.dst, err = load(step.names...); err != nil {
			return nil, err
		}
	}

	s.counts = [numStates]int{
		Idle:          len(s.idle),
		Wander:        len(s.walkR),
		Follow:        len(s.walkR),
		FallingAsleep: len(s.sleep),
		Asleep:        len(s.zz),
		WakingUp:      len(s.sleep),
		Angry:         len(s.angry),
		BeingPetted:   len(s.blush),
	}
	return &s, nil
}

// Icon is the image used for the tray and the app icon.
func (s *Sprites) Icon() fyne.Resource { return s.idle[0] }

// Frame picks the sprite for a rendered view. The frame index is taken modulo
// the chosen set, so a state whose sets differ in length (Follow walks with
// four frames but sits with the idle set) can never index out of range.
func (s *Sprites) Frame(v View) fyne.Resource {
	var set []fyne.Resource

	switch v.State {
	case Wander:
		set = s.walk(v.Facing)
	case Follow:
		if v.Moving {
			set = s.walk(v.Facing)
		} else {
			set = s.idle // sitting next to the cursor, watching
		}
	case FallingAsleep:
		set = s.sleep
	case WakingUp:
		// The wake-up animation is the lie-down one played backwards.
		set = s.sleep
		if n := len(set); n > 0 {
			return set[n-1-(v.Frame%n)]
		}
	case Asleep:
		set = s.zz
	case Angry:
		set = s.angry
	case BeingPetted:
		set = s.blush
	default:
		set = s.idle
	}

	if len(set) == 0 {
		return s.idle[0]
	}
	return set[v.Frame%len(set)]
}

func (s *Sprites) walk(facing int) []fyne.Resource {
	if facing < 0 {
		return s.walkL
	}
	return s.walkR
}
