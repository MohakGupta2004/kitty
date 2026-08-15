package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("shipped defaults are invalid: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := map[string]func(*Config){
		"tick too small":     func(c *Config) { c.TickMS = 1 },
		"negative speed":     func(c *Config) { c.WalkSpeed = -1 },
		"give up too close":  func(c *Config) { c.GiveUpRadius = c.FollowRadius - 1 },
		"chance over 100":    func(c *Config) { c.FollowChance = 101 },
		"awake range empty":  func(c *Config) { c.AwakeMaxSec = c.AwakeMinSec - 1 },
		"sleep range empty":  func(c *Config) { c.SleepMaxSec = c.SleepMinSec - 1 },
		"scale out of range": func(c *Config) { c.Scale = 10 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := DefaultConfig()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadConfigWritesDefaultsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg != DefaultConfig() {
		t.Fatal("expected the defaults")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default config was not written: %v", err)
	}

	// Reading it back has to give the same thing.
	again, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig on the written file: %v", err)
	}
	if again != cfg {
		t.Fatalf("round trip changed the config:\n got %+v\nwant %+v", again, cfg)
	}
}

func TestLoadConfigFillsInMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"follow_chance": 10, "scale": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.FollowChance != 10 || cfg.Scale != 2 {
		t.Fatalf("file values were not applied: %+v", cfg)
	}
	if cfg.WalkSpeed != DefaultConfig().WalkSpeed {
		t.Fatalf("absent keys should keep their default, got walk_speed=%v", cfg.WalkSpeed)
	}
}

func TestLoadConfigRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"malformed.json": `{ not json`,
		"invalid.json":   `{"scale": 99}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(path)
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if cfg != DefaultConfig() {
			t.Fatalf("%s: a broken file should fall back to the defaults", name)
		}
	}
}

func TestTicksNeverReturnsZero(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.ticks(0); got != 1 {
		t.Fatalf("ticks(0) = %d, want 1", got)
	}
	if got := cfg.ticks(3); got != 30 {
		t.Fatalf("ticks(3) = %d, want 30 at a 100ms tick", got)
	}
}
