package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcopollivier/project.awtrix/internal/awtrix"
	"github.com/marcopollivier/project.awtrix/internal/config"
)

type fakeDevice struct {
	calls     []string
	apps      []awtrix.AppInfo
	busy      int               // answer 503 this many times
	installed map[string]string // what GET /api/v1/apps/script/{name} returns
	moduleCfg bool              // accept module settings (the real NG 1.2.2 does)
}

func (f *fakeDevice) Script(_ context.Context, _, name string) ([]byte, bool, error) {
	src, ok := f.installed[name]
	return []byte(src), ok, nil
}

func (f *fakeDevice) Reboot(context.Context, string) error {
	f.calls = append(f.calls, "reboot")
	return nil
}

func (f *fakeDevice) PutScript(_ context.Context, _, name string, src []byte) error {
	if f.busy > 0 {
		f.busy--
		return &awtrix.StatusError{Status: 503}
	}
	f.calls = append(f.calls, "put "+name)
	return nil
}

func (f *fakeDevice) PatchAppConfig(_ context.Context, _, name string, v map[string]any) error {
	if name == "counter" && !f.moduleCfg {
		return &awtrix.StatusError{Status: 404}
	}
	f.calls = append(f.calls, fmt.Sprintf("config %s %v", name, v))
	return nil
}

func (f *fakeDevice) Apps(context.Context, string) ([]awtrix.AppInfo, error) { return f.apps, nil }

func writeScripts(t *testing.T, files map[string]string) string {
	dir := t.TempDir()
	for name, src := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	}
	return dir
}

func TestLoadPutsModulesFirst(t *testing.T) {
	dir := writeScripts(t, map[string]string{
		"github.ax":  "# @requires counter\nreturn G()",
		"counter.ax": "# @name Counter\n# @module\nreturn m",
		"notes.txt":  "ignored",
	})
	scripts, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 2 || scripts[0].Name != "counter" || !scripts[0].Module || scripts[1].Name != "github" {
		t.Fatalf("order: %+v", scripts)
	}
}

func TestDeploy(t *testing.T) {
	Retry = 0
	dir := writeScripts(t, map[string]string{
		"counter.ax":   "# @module\nreturn m",
		"github.ax":    "return G()",
		"youtube.ax":   "return Y()",
		"linkedin.ax":  "return L()",
		"instagram.ax": "return I()",
	})
	scripts, _ := Load(dir)
	cfg := config.Default()
	cfg.Sources.GitHub.Enabled, cfg.Sources.GitHub.User, cfg.Sources.GitHub.Milestones.Step = true, "marco", 100
	cfg.Sources.LinkedIn.Enabled = true
	cfg.Sources.YouTube.Enabled = false
	cfg.Sources.Instagram.Enabled = true // no token: the key is not sent
	followers := int64(7500)

	d := &fakeDevice{busy: 1, apps: []awtrix.AppInfo{
		{Name: "github", Origin: "script", InLoop: true},
		{Name: "instagram", Origin: "script", InLoop: true},
		{Name: "linkedin", Origin: "script", Error: &awtrix.AppError{Message: "boom", Line: 7, Hook: "setup"}},
	}}
	err := Deploy(context.Background(), d, "http://clock", scripts, Settings(cfg, &followers), Options{}, slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), "linkedin: linha 7 (setup): boom") {
		t.Fatalf("expected linkedin error, got %v", err)
	}
	got := strings.Join(d.calls, "\n")
	want := strings.Join([]string{
		"put counter", "put github", "put instagram", "put linkedin",
		"config github map[icon:21802 interval_min:30 step:100 user:marco]",
		"config instagram map[graph_version:v26.0 icon:36659 interval_min:30 step:0]",
		"config linkedin map[followers:7500 icon:32288 stale_days:30 step:0]",
		"reboot", // counter was installed
	}, "\n")
	if got != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", got, want)
	}
}

func TestDeploySkipsUnchangedScriptsAndRebootsOnlyForModules(t *testing.T) {
	Retry = 0
	files := map[string]string{
		"counter.ax":  "# @module\nreturn m",
		"github.ax":   "return G2()",
		"linkedin.ax": "return L()",
	}
	scripts, _ := Load(writeScripts(t, files))
	cfg := config.Default()
	cfg.Sources.GitHub.Enabled, cfg.Sources.LinkedIn.Enabled = true, true
	apps := []awtrix.AppInfo{{Name: "github", InLoop: true}, {Name: "linkedin", InLoop: true}}
	run := func(d *fakeDevice, opts Options) string {
		if err := Deploy(context.Background(), d, "http://clock", scripts, Settings(cfg, nil), opts, slog.New(slog.DiscardHandler)); err != nil {
			t.Fatal(err)
		}
		return strings.Join(d.calls, "\n")
	}
	clock := map[string]string{"counter.ax": "", "counter": files["counter.ax"], "github": "return G1()", "linkedin": files["linkedin.ax"]}

	// Only github changed: it alone is reinstalled, every app still gets its
	// settings, and an app change does not reboot.
	got := run(&fakeDevice{apps: apps, installed: clock}, Options{Only: []string{"github", "linkedin"}})
	want := "put github\n" +
		"config github map[icon:21802 interval_min:30 step:0 user:]\n" +
		"config linkedin map[icon:32288 stale_days:30 step:0]"
	if got != want {
		t.Fatalf("only github changed:\n%s\nwant:\n%s", got, want)
	}

	// A full deploy configures the unchanged module, which makes every script
	// reload it: no reinstall, but a reboot.
	if got := run(&fakeDevice{apps: apps, installed: clock, moduleCfg: true}, Options{}); !strings.HasSuffix(got, "reboot") || strings.Contains(got, "put counter") {
		t.Fatalf("unchanged counter configured: no reinstall, but a reboot:\n%s", got)
	}
	// If the module's settings are refused, nothing changed on the clock.
	if got := run(&fakeDevice{apps: apps, installed: clock}, Options{}); strings.Contains(got, "reboot") {
		t.Fatalf("module untouched, yet rebooted:\n%s", got)
	}

	// --force reinstalls even what is unchanged; --no-reboot skips the reboot.
	got = run(&fakeDevice{apps: apps, installed: clock}, Options{Force: true, NoReboot: true})
	if !strings.Contains(got, "put counter") || !strings.Contains(got, "put linkedin") || strings.Contains(got, "reboot") {
		t.Fatalf("force + no-reboot:\n%s", got)
	}
}
