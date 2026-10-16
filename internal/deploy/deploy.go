// Package deploy installs the Berry scripts (scripts/*.ax) on a clock running
// AWTRIX NG and fills their @config settings from config.yaml and .env, so the
// clock fetches its counters by itself, without the hub.
package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
)

// Script is one file of scripts_dir.
type Script struct {
	Name   string // install name = file name without .ax
	Source []byte
	Module bool // # @module: shared code, installed before the apps that import it
}

// Load reads every .ax file of dir, modules first.
func Load(dir string) ([]Script, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.ax"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("nenhum script em %s", dir)
	}
	var out []Script
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, Script{
			Name:   strings.TrimSuffix(filepath.Base(p), ".ax"),
			Source: src,
			Module: bytes.Contains(src, []byte("\n# @module")) || bytes.HasPrefix(src, []byte("# @module")),
		})
	}
	slices.SortStableFunc(out, func(a, b Script) int {
		switch {
		case a.Module == b.Module:
			return strings.Compare(a.Name, b.Name)
		case a.Module:
			return -1
		}
		return 1
	})
	return out, nil
}

// Settings maps each script to its @config values. Keys must match the
// "# @config <key>" lines of the scripts. A script missing from the map is
// installed without settings (e.g. a module without @config); enabled says
// whether the source is turned on in config.yaml.
//
// Secrets are only sent when set, so a token typed in the clock's web UI is not
// wiped by a deploy without .env. The "source" setting (auto/direct/hub) is
// never sent: it stays as chosen on the clock.
func Settings(cfg *config.Config, linkedinFollowers *int64) map[string]Setting {
	sc := cfg.Sources
	mins := func(d time.Duration) int { return max(1, int(d.Minutes())) }
	secret := func(m map[string]any, key, v string) map[string]any {
		if v != "" {
			m[key] = v
		}
		return m
	}
	counter := map[string]any{
		"hub_url":    cfg.API.PublicURL,
		"text_color": cfg.Colors.Text,
		"up_color":   cfg.Colors.Up,
		"down_color": cfg.Colors.Down,
		"bar_color":  cfg.Colors.Bar,
		"party_icon": cfg.Icon("party"),
	}
	linkedin := map[string]any{
		"icon":       cfg.Icon("linkedin"),
		"step":       sc.LinkedIn.Milestones.Step,
		"stale_days": int(sc.LinkedIn.StaleAfter.Hours() / 24),
	}
	if linkedinFollowers != nil {
		linkedin["followers"] = *linkedinFollowers
	}
	return map[string]Setting{
		"counter": {Enabled: true, Values: counter},
		"github": {Enabled: sc.GitHub.Enabled, Values: secret(map[string]any{
			"user": sc.GitHub.User, "icon": cfg.Icon("github"),
			"interval_min": mins(sc.GitHub.Interval), "step": sc.GitHub.Milestones.Step,
		}, "token", sc.GitHub.Token)},
		"github_stars": {Enabled: sc.GitHub.Enabled, Values: secret(map[string]any{
			"user": sc.GitHub.User, "icon": cfg.Icon("stars"),
			"interval_min": mins(sc.GitHub.Interval), "step": sc.GitHub.Stars.Step,
		}, "token", sc.GitHub.Token)},
		"youtube": {Enabled: sc.YouTube.Enabled, Values: secret(map[string]any{
			"handle": sc.YouTube.Handle, "icon": cfg.Icon("youtube"),
			"interval_min": mins(sc.YouTube.Interval), "step": sc.YouTube.Milestones.Step,
		}, "api_key", sc.YouTube.APIKey)},
		"instagram": {Enabled: sc.Instagram.Enabled, Values: secret(map[string]any{
			"graph_version": sc.Instagram.GraphVer, "icon": cfg.Icon("instagram"),
			"interval_min": mins(sc.Instagram.Interval), "step": sc.Instagram.Milestones.Step,
		}, "token", sc.Instagram.Token)},
		"linkedin": {Enabled: sc.LinkedIn.Enabled, Values: linkedin},
	}
}

type Setting struct {
	Enabled bool
	Values  map[string]any
}

// Device is what Deploy needs from awtrix.Client.
type Device interface {
	PutScript(ctx context.Context, target, name string, src []byte) error
	PatchAppConfig(ctx context.Context, target, name string, values map[string]any) error
	Apps(ctx context.Context, target string) ([]awtrix.AppInfo, error)
}

// Retry is how long to wait when the clock answers 503 (a script is busy).
var Retry = 2 * time.Second

// Deploy installs scripts on target, applies settings and reports each
// script's state. only (optional) limits it to these install names.
func Deploy(ctx context.Context, d Device, target string, scripts []Script, settings map[string]Setting, only []string, log *slog.Logger) error {
	var installed []string
	for _, s := range scripts {
		set, known := settings[s.Name]
		if len(only) > 0 && !slices.Contains(only, s.Name) {
			continue
		}
		if known && !set.Enabled {
			log.Info("pulando (fonte desligada na config)", "script", s.Name)
			continue
		}
		if err := retry(ctx, func() error { return d.PutScript(ctx, target, s.Name, s.Source) }); err != nil {
			return fmt.Errorf("instalar %s: %w", s.Name, err)
		}
		log.Info("script instalado", "script", s.Name, "bytes", len(s.Source))
		installed = append(installed, s.Name)
	}
	for _, name := range installed {
		vals := settings[name].Values
		if len(vals) == 0 {
			continue
		}
		if err := retry(ctx, func() error { return d.PatchAppConfig(ctx, target, name, vals) }); err != nil {
			if isModule(scripts, name) {
				// The docs show module settings on the Apps tab but do not say
				// whether the config route takes a module: do not stop the deploy.
				log.Warn("configuração do módulo não aplicada; ajuste na aba Apps do relógio", "script", name, "err", err)
				continue
			}
			return fmt.Errorf("configurar %s: %w", name, err)
		}
		log.Info("configuração aplicada", "script", name, "chaves", len(vals))
	}
	return report(ctx, d, target, scripts, installed, log)
}

// report checks GET /api/v1/apps for compile or runtime errors.
func report(ctx context.Context, d Device, target string, scripts []Script, installed []string, log *slog.Logger) error {
	apps, err := d.Apps(ctx, target)
	if err != nil {
		return fmt.Errorf("listar apps: %w", err)
	}
	var errs []error
	for _, name := range installed {
		i := slices.IndexFunc(apps, func(a awtrix.AppInfo) bool { return a.Name == name })
		switch {
		case i < 0 && isModule(scripts, name):
			// modules are not apps: they do not show up in the rotation
		case i < 0:
			errs = append(errs, fmt.Errorf("%s: não aparece em /api/v1/apps", name))
		case apps[i].Error != nil:
			e := apps[i].Error
			errs = append(errs, fmt.Errorf("%s: linha %d (%s): %s", name, e.Line, e.Hook, e.Message))
		default:
			log.Info("ok", "script", name, "na rotação", apps[i].InLoop)
		}
	}
	return errors.Join(errs...)
}

func isModule(scripts []Script, name string) bool {
	return slices.ContainsFunc(scripts, func(s Script) bool { return s.Name == name && s.Module })
}

func retry(ctx context.Context, fn func() error) error {
	err := fn()
	for range 3 {
		se, ok := errors.AsType[*awtrix.StatusError](err)
		if !ok || se.Status != http.StatusServiceUnavailable {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(Retry):
		}
		err = fn()
	}
	return err
}
