package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Config holds every tunable knob. It is read from (and, when missing, written
// to) a JSON file in the user config directory, so behaviour can be changed
// without a rebuild.
type Config struct {
	// Timing
	TickMS int `json:"tick_ms"` // simulation step, 100 is one animation frame

	// Movement, in points per tick
	WalkSpeed   float64 `json:"walk_speed"`
	FollowSpeed float64 `json:"follow_speed"`
	FleeSpeed   float64 `json:"flee_speed"`

	// Following
	FollowRadius   float64 `json:"follow_radius"`    // notices the cursor within this distance
	GiveUpRadius   float64 `json:"give_up_radius"`   // stops chasing past this distance
	FollowStandoff float64 `json:"follow_standoff"`  // sits down this far from the cursor
	FollowChance   int     `json:"follow_chance"`    // 0-100, how often it bothers to follow
	FollowMaxSec   int     `json:"follow_max_sec"`   // gets bored after this long
	CursorStillSec int     `json:"cursor_still_sec"` // a parked cursor stops being interesting

	// Sleep schedule. The cat is awake for a random span in [awake_min, awake_max],
	// then sleeps for a random span in [sleep_min, sleep_max] and ignores the
	// cursor completely while it does.
	AwakeMinSec int `json:"awake_min_sec"`
	AwakeMaxSec int `json:"awake_max_sec"`
	SleepMinSec int `json:"sleep_min_sec"`
	SleepMaxSec int `json:"sleep_max_sec"`

	// Petting
	PetsBeforeAnnoyed int `json:"pets_before_annoyed"`
	PetPatienceSec    int `json:"pet_patience_sec"` // pet counter decays after this idle time

	// Window
	Scale       float64 `json:"scale"` // sprite scale, 1 = 72x64 points
	AlwaysOnTop bool    `json:"always_on_top"`
	Tray        bool    `json:"tray"` // show the system tray menu
}

// DefaultConfig returns the tuning the cat ships with.
func DefaultConfig() Config {
	return Config{
		TickMS: 100,

		WalkSpeed:   3,
		FollowSpeed: 5,
		FleeSpeed:   9,

		FollowRadius:   520,
		GiveUpRadius:   900,
		FollowStandoff: 60,
		FollowChance:   70,
		FollowMaxSec:   25,
		CursorStillSec: 4,

		AwakeMinSec: 90,
		AwakeMaxSec: 210,
		SleepMinSec: 45,
		SleepMaxSec: 120,

		PetsBeforeAnnoyed: 6,
		PetPatienceSec:    6,

		Scale:       1,
		AlwaysOnTop: true,
		Tray:        true,
	}
}

// Tick is the simulation step duration.
func (c Config) Tick() time.Duration { return time.Duration(c.TickMS) * time.Millisecond }

// ticks converts seconds into simulation steps, never returning zero so that
// "every N ticks" arithmetic stays safe.
func (c Config) ticks(seconds int) int {
	n := seconds * 1000 / max(c.TickMS, 1)
	return max(n, 1)
}

// Validate reports the first value that would make the simulation misbehave.
// Zero values are rejected rather than silently repaired so a typo in the
// config file is visible instead of mysterious.
func (c Config) Validate() error {
	switch {
	case c.TickMS < 10 || c.TickMS > 1000:
		return fmt.Errorf("tick_ms must be between 10 and 1000, got %d", c.TickMS)
	case c.WalkSpeed <= 0 || c.FollowSpeed <= 0 || c.FleeSpeed <= 0:
		return errors.New("walk_speed, follow_speed and flee_speed must all be positive")
	case c.FollowRadius <= 0:
		return errors.New("follow_radius must be positive")
	case c.GiveUpRadius < c.FollowRadius:
		return errors.New("give_up_radius must be at least follow_radius")
	case c.FollowStandoff < 0:
		return errors.New("follow_standoff must not be negative")
	case c.FollowChance < 0 || c.FollowChance > 100:
		return fmt.Errorf("follow_chance must be between 0 and 100, got %d", c.FollowChance)
	case c.FollowMaxSec <= 0 || c.CursorStillSec <= 0:
		return errors.New("follow_max_sec and cursor_still_sec must be positive")
	case c.AwakeMinSec <= 0 || c.AwakeMaxSec < c.AwakeMinSec:
		return errors.New("awake_min_sec must be positive and awake_max_sec must not be smaller")
	case c.SleepMinSec <= 0 || c.SleepMaxSec < c.SleepMinSec:
		return errors.New("sleep_min_sec must be positive and sleep_max_sec must not be smaller")
	case c.PetsBeforeAnnoyed <= 0 || c.PetPatienceSec <= 0:
		return errors.New("pets_before_annoyed and pet_patience_sec must be positive")
	case c.Scale < 0.5 || c.Scale > 4:
		return fmt.Errorf("scale must be between 0.5 and 4, got %v", c.Scale)
	}
	return nil
}

// ConfigPath is where the config file lives, e.g.
// ~/Library/Application Support/desktop-kitty/config.json on macOS.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(dir, appDirName, "config.json"), nil
}

// LoadConfig reads path, filling in any field the file leaves out from the
// defaults. A missing file is not an error: the defaults are returned and
// written back so there is something to edit.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := SaveConfig(path, cfg); err != nil {
			// A read-only home directory is not worth refusing to start over.
			return cfg, fmt.Errorf("write default config: %w", err)
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}

	// Unmarshalling onto the defaults leaves absent keys at their default,
	// so old config files keep working when new knobs are added.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return DefaultConfig(), fmt.Errorf("invalid config in %s: %w", path, err)
	}
	return cfg, nil
}

// SaveConfig writes cfg to path, creating the directory and replacing the file
// atomically so an interrupted write cannot leave a half-written config.
func SaveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
