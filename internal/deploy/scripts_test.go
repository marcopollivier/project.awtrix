package deploy

import (
	"regexp"
	"slices"
	"testing"

	"github.com/marcopollivier/project.awtrix/internal/config"
)

// The real scripts/ must line up with Settings: every key deploy sends needs a
// "# @config <key>" line in that script, or the clock answers 422.
func TestScriptsMatchSettings(t *testing.T) {
	scripts, err := Load("../../scripts")
	if err != nil {
		t.Fatal(err)
	}
	if scripts[0].Name != "counter" || !scripts[0].Module {
		t.Fatalf("counter must be the first, as a module: %+v", scripts[0].Name)
	}
	cfg := config.Default()
	cfg.Sources.GitHub.Token, cfg.Sources.YouTube.APIKey, cfg.Sources.Instagram.Token = "x", "x", "x"
	cfg.Sources.TikTok.ClientKey, cfg.Sources.TikTok.ClientSecret = "x", "x"
	n := int64(1)
	settings := Settings(cfg, &n)
	keyRe := regexp.MustCompile(`(?m)^# @config\s+(\S+)`)
	for name, set := range settings {
		i := slices.IndexFunc(scripts, func(s Script) bool { return s.Name == name })
		if i < 0 {
			t.Errorf("no scripts/%s.ax for settings %v", name, set.Values)
			continue
		}
		var keys []string
		for _, m := range keyRe.FindAllStringSubmatch(string(scripts[i].Source), -1) {
			keys = append(keys, m[1])
		}
		for k := range set.Values {
			if !slices.Contains(keys, k) {
				t.Errorf("%s: deploy sends %q but the script has no @config for it (has %v)", name, k, keys)
			}
		}
	}
}
