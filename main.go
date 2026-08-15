// Command kitty puts a small cat on the desktop. It wanders around, follows the
// mouse cursor when it feels like it, naps on its own schedule, and gets
// annoyed if you pet it too much.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

const (
	appID      = "com.mohakgupta.desktopkitty"
	appDirName = "desktop-kitty"
)

// Build information, set by the release build:
//
//	go build -ldflags "-X main.version=v1.0.0 -X main.commit=abc1234 -X main.date=..."
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// screenCheckInterval is how often the display is re-measured.
const screenCheckInterval = time.Minute

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		configPath  = flag.String("config", "", "path to the config file (default: user config dir)")
		noFollow    = flag.Bool("no-follow", false, "start with cursor following turned off")
		scale       = flag.Float64("scale", 0, "sprite scale, 0.5 to 4 (overrides the config file)")
		verbose     = flag.Bool("verbose", false, "log every state change")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("kitty %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	if err := run(*configPath, *noFollow, *scale); err != nil {
		log.Error("kitty stopped", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, noFollow bool, scaleOverride float64) error {
	if configPath == "" {
		p, err := ConfigPath()
		if err != nil {
			return err
		}
		configPath = p
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		// A bad or unwritable config is not fatal: say so and use the defaults.
		slog.Warn("using default config", "path", configPath, "err", err)
	} else {
		slog.Info("config loaded", "path", configPath)
	}
	if scaleOverride != 0 {
		cfg.Scale = scaleOverride
		if err := cfg.Validate(); err != nil {
			return err
		}
	}

	sprites, err := LoadSprites()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	screenW, screenH, err := screenSize(ctx)
	if err != nil {
		slog.Warn("could not measure the display, assuming a default size",
			"width", screenW, "height", screenH, "err", err)
	}

	if _, ok := cursorPos(); !ok {
		slog.Warn("this system does not report the cursor position, so the cat will only wander")
	}

	w, h := float32(cfg.Scale)*catW, float32(cfg.Scale)*catH
	cat := NewCat(cfg, screenW, screenH, w, h, sprites.counts, rand.New(rand.NewSource(time.Now().UnixNano())))
	cat.SetFollow(!noFollow)

	a := app.NewWithID(appID)
	a.SetIcon(sprites.Icon())
	a.Settings().SetTheme(clearTheme{theme.DefaultTheme()})

	win := newWindow(a, cfg, cat, sprites, w, h)
	if cfg.Tray {
		setupTray(a, cat)
	}

	go watchScreenSize(ctx, screenCheckInterval, screenW, screenH, func(nw, nh float32) {
		slog.Info("display changed", "width", nw, "height", nh)
		cat.Resize(nw, nh)
	})
	go animate(ctx, cfg, cat, sprites, win)

	// Ctrl-C and SIGTERM should close the window the same way the tray Quit
	// item does, instead of killing the process mid-frame.
	go func() {
		<-ctx.Done()
		fyne.Do(a.Quit)
	}()

	win.ShowAndRun()
	return nil
}

// newWindow creates the borderless, transparent, always-on-top window the cat
// lives in.
func newWindow(a fyne.App, cfg Config, cat *Cat, sprites *Sprites, w, h float32) fyne.Window {
	var win fyne.Window
	if drv, ok := a.Driver().(desktop.Driver); ok {
		win = drv.CreateSplashWindow() // borderless, no title bar
		makeWindowTransparent()        // after GLFW init, before the window opens
	} else {
		win = a.NewWindow("kitty")
	}

	view := cat.View()
	if dw, ok := win.(desktop.Window); ok {
		if cfg.AlwaysOnTop {
			dw.RequestAlwaysOnTop()
		}
		dw.RequestPosition(int(view.X), int(view.Y))
	}

	win.SetContent(newCatWidget(cat, sprites, w, h))
	win.SetPadded(false)
	win.Resize(fyne.NewSize(w, h))
	win.SetFixedSize(true)
	return win
}

// animate is the simulation loop: one Step per tick, then a repaint and a
// window move on the UI thread.
func animate(ctx context.Context, cfg Config, cat *Cat, sprites *Sprites, win fyne.Window) {
	t := time.NewTicker(cfg.Tick())
	defer t.Stop()

	last := cat.View()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		pos, ok := cursorPos()
		cat.Step(pos, ok)

		view := cat.View()
		if view.State != last.State {
			slog.Debug("state", "from", last.State, "to", view.State)
		}
		res := sprites.Frame(view)
		moved := int(view.X) != int(last.X) || int(view.Y) != int(last.Y)
		last = view

		// Every UI call has to happen on the main thread.
		fyne.Do(func() {
			cw, _ := win.Content().(*catWidget)
			if cw != nil {
				cw.setFrame(res)
			}
			if dw, ok := win.(desktop.Window); ok && moved {
				dw.RequestPosition(int(view.X), int(view.Y))
			}
		})
	}
}

// setupTray adds the menu bar / notification area menu: it is the only way to
// quit an app whose window has no title bar.
func setupTray(a fyne.App, cat *Cat) {
	desk, ok := a.(desktop.App)
	if !ok {
		return
	}

	follow := fyne.NewMenuItem("Follow the cursor", nil)
	follow.Checked = cat.Following()

	nap := fyne.NewMenuItem("Take a nap", nil)

	menu := fyne.NewMenu("kitty", follow, nap)

	follow.Action = func() {
		cat.SetFollow(!cat.Following())
		follow.Checked = cat.Following()
		menu.Refresh()
	}
	nap.Action = func() {
		if cat.Sleeping() {
			cat.Wake()
		} else {
			cat.Nap()
		}
		menu.Refresh()
	}
	// Keep the nap item's label in step with what it will actually do.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			want := "Take a nap"
			if cat.Sleeping() {
				want = "Wake up"
			}
			if nap.Label != want {
				fyne.Do(func() {
					nap.Label = want
					menu.Refresh()
				})
			}
		}
	}()

	desk.SetSystemTrayMenu(menu)
}
