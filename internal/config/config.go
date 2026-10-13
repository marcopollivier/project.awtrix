// Package config loads config.yaml and secrets from the environment / .env.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Targets  []string          `yaml:"targets"`
	Timezone string            `yaml:"timezone"`
	DataDir  string            `yaml:"data_dir"`
	Defaults Defaults          `yaml:"defaults"`
	Icons    map[string]string `yaml:"icons"`
	IconsDir string            `yaml:"icons_dir"` // project icons (<name>.gif), relative to the config file
	Colors   Colors            `yaml:"colors"`
	Sim      Sim               `yaml:"sim"`
	Sources  Sources           `yaml:"sources"`
	Location *time.Location    `yaml:"-"`
}

type Defaults struct {
	Duration int `yaml:"duration"` // seconds each app stays on screen
	Lifetime int `yaml:"lifetime"` // seconds without update before the clock drops the app
}

type Colors struct {
	Text string `yaml:"text"`
	Up   string `yaml:"up"`
	Down string `yaml:"down"`
	Bar  string `yaml:"bar"`
}

type Sim struct {
	Addr string `yaml:"addr"`
}

type Milestones struct {
	Step int64   `yaml:"step"`
	List []int64 `yaml:"list"`
}

type Sources struct {
	GitHub    GitHub    `yaml:"github"`
	YouTube   YouTube   `yaml:"youtube"`
	Instagram Instagram `yaml:"instagram"`
	LinkedIn  LinkedIn  `yaml:"linkedin"`
	Football  Football  `yaml:"football"`
}

type GitHub struct {
	Enabled    bool          `yaml:"enabled"`
	User       string        `yaml:"user"`
	Interval   time.Duration `yaml:"interval"`
	Milestones Milestones    `yaml:"milestones"`
	Stars      Milestones    `yaml:"stars_milestones"`
	Token      string        `yaml:"-"` // GITHUB_TOKEN
}

type YouTube struct {
	Enabled      bool          `yaml:"enabled"`
	Handle       string        `yaml:"handle"`
	Interval     time.Duration `yaml:"interval"`
	Milestones   Milestones    `yaml:"milestones"`
	LatestWindow time.Duration `yaml:"latest_video_window"` // show latest video views for this long after publishing
	APIKey       string        `yaml:"-"`                   // YOUTUBE_API_KEY
}

type Instagram struct {
	Enabled    bool          `yaml:"enabled"`
	Interval   time.Duration `yaml:"interval"`
	Milestones Milestones    `yaml:"milestones"`
	GraphVer   string        `yaml:"graph_version"`
	Token      string        `yaml:"-"` // INSTAGRAM_TOKEN (seed; refreshed copies live in data_dir)
}

type LinkedIn struct {
	Enabled    bool          `yaml:"enabled"`
	Milestones Milestones    `yaml:"milestones"`
	StaleAfter time.Duration `yaml:"stale_after"`
}

type Football struct {
	Enabled bool     `yaml:"enabled"`
	TeamID  string   `yaml:"team_id"`
	Abbrev  string   `yaml:"abbrev"`
	Name    string   `yaml:"name"`
	Leagues []string `yaml:"leagues"`
	Fixture string   `yaml:"-"` // --football-fixture: replay a directory of summaries instead of ESPN
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	loadDotEnv(filepath.Join(filepath.Dir(path), ".env"))
	if !filepath.IsAbs(c.IconsDir) {
		c.IconsDir = filepath.Join(filepath.Dir(path), c.IconsDir)
	}
	c.Sources.GitHub.Token = os.Getenv("GITHUB_TOKEN")
	c.Sources.YouTube.APIKey = os.Getenv("YOUTUBE_API_KEY")
	c.Sources.Instagram.Token = os.Getenv("INSTAGRAM_TOKEN")
	return c, c.Finish()
}

// Default is used as the base for config.yaml and when no config file exists (e.g. `sim`).
func Default() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Targets:  []string{"http://localhost:7070"},
		Timezone: "America/Sao_Paulo",
		DataDir:  filepath.Join(home, ".awtrix-hub"),
		Defaults: Defaults{Duration: 7, Lifetime: 3600},
		Colors:   Colors{Text: "#FFFFFF", Up: "#00FF66", Down: "#FF3333", Bar: "#00A2FF"},
		Sim:      Sim{Addr: "localhost:7070"},
		IconsDir: "icons",
		Icons: map[string]string{
			"github": "21802", "stars": "gh_stars", "youtube": "youtube", "instagram": "36659",
			"linkedin": "32288", "football": "26316", "goal": "8530", "party": "9554",
		},
		Sources: Sources{
			GitHub:    GitHub{Interval: 30 * time.Minute},
			YouTube:   YouTube{Interval: 15 * time.Minute, LatestWindow: 48 * time.Hour},
			Instagram: Instagram{Interval: 30 * time.Minute, GraphVer: "v26.0"},
			LinkedIn:  LinkedIn{StaleAfter: 30 * 24 * time.Hour},
			Football:  Football{TeamID: "3454", Abbrev: "VAS", Name: "VASCO", Leagues: []string{"bra.1"}},
		},
	}
}

// Finish validates derived fields (timezone, data dir); Load calls it.
func (c *Config) Finish() error {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	c.Location = loc
	if strings.HasPrefix(c.DataDir, "~/") {
		home, _ := os.UserHomeDir()
		c.DataDir = filepath.Join(home, c.DataDir[2:])
	}
	return nil
}

// Icon returns the configured icon id for a key ("" if none).
func (c *Config) Icon(key string) string { return c.Icons[key] }

// loadDotEnv sets KEY=VALUE pairs that are not already in the environment.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}
