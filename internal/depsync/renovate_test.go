package depsync

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// renovateConfigPath is the repository's Renovate configuration, relative to this package.
const renovateConfigPath = "../../renovate.json"

// renovateConfig holds the parts of renovate.json these checks read.
type renovateConfig struct {
	CustomManagers []struct {
		CustomType          string   `json:"customType"`
		ManagerFilePatterns []string `json:"managerFilePatterns"`
		MatchStrings        []string `json:"matchStrings"`
		DepNameTemplate     string   `json:"depNameTemplate"`
		DatasourceTemplate  string   `json:"datasourceTemplate"`
	} `json:"customManagers"`
	PackageRules []struct {
		MatchPackageNames []string          `json:"matchPackageNames"`
		MatchManagers     []string          `json:"matchManagers"`
		GroupName         string            `json:"groupName"`
		Constraints       map[string]string `json:"constraints"`
	} `json:"packageRules"`
}

func readRenovateConfig(t *testing.T) renovateConfig {
	t.Helper()

	raw, err := os.ReadFile(renovateConfigPath)
	if err != nil {
		t.Fatalf("read renovate.json: %v", err)
	}

	var cfg renovateConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse renovate.json: %v", err)
	}
	return cfg
}

// TestRenovateGroupsGovueHalves fails when renovate.json no longer puts the Go module and
// the npm package of govue into one group. Without the group Renovate raises the halves in
// separate pull requests, and each fails check:govue-version.
func TestRenovateGroupsGovueHalves(t *testing.T) {
	t.Parallel()

	cfg := readRenovateConfig(t)

	var groups []string
	for _, rule := range cfg.PackageRules {
		if rule.GroupName == "" {
			continue
		}
		if slices.Contains(rule.MatchPackageNames, goModulePath) &&
			slices.Contains(rule.MatchPackageNames, "ostendorf-cloud/govue") {
			groups = append(groups, rule.GroupName)
		}
	}

	if len(groups) != 1 {
		t.Fatalf("want exactly one packageRules group naming both %q and %q, got %d: %v",
			goModulePath, "ostendorf-cloud/govue", len(groups), groups)
	}
}

// TestRenovateManagerReadsGovueSpecifier fails when the custom manager's expression no
// longer extracts the tag from web/package.json's real specifier. Renovate's own npm
// manager skips that specifier ("unversioned-reference") because of the "&path:/web"
// suffix, so this manager is the only way the npm half is ever updated.
func TestRenovateManagerReadsGovueSpecifier(t *testing.T) {
	t.Parallel()

	cfg := readRenovateConfig(t)

	raw, err := os.ReadFile(packageJSONPath)
	if err != nil {
		t.Fatalf("read web/package.json: %v", err)
	}
	wantTag, err := packageJSONGovueTag(t)
	if err != nil {
		t.Fatalf("resolve the tag in web/package.json: %v", err)
	}

	for _, manager := range cfg.CustomManagers {
		if manager.CustomType != "regex" || manager.DepNameTemplate != "ostendorf-cloud/govue" {
			continue
		}
		if manager.DatasourceTemplate != "github-tags" {
			t.Fatalf("datasourceTemplate = %q, want github-tags", manager.DatasourceTemplate)
		}
		if !slices.Contains(manager.ManagerFilePatterns, "/^web/package\\.json$/") {
			t.Fatalf("managerFilePatterns = %v, want web/package.json", manager.ManagerFilePatterns)
		}
		if len(manager.MatchStrings) != 1 {
			t.Fatalf("want one matchStrings entry, got %d", len(manager.MatchStrings))
		}

		re := regexp.MustCompile(manager.MatchStrings[0])
		group := re.SubexpIndex("currentValue")
		if group < 0 {
			t.Fatalf("matchStrings %q has no named group currentValue", manager.MatchStrings[0])
		}
		match := re.FindSubmatch(raw)
		if match == nil {
			t.Fatalf("matchStrings %q finds nothing in web/package.json", manager.MatchStrings[0])
		}
		got := string(match[group])
		if got != wantTag {
			t.Fatalf("custom manager extracts tag %q, web/package.json pins %q", got, wantTag)
		}
		return
	}

	t.Fatal("renovate.json has no regex custom manager for ostendorf-cloud/govue")
}

// misePnpm reads the pnpm pin of mise.toml.
func misePnpm(t *testing.T) string {
	t.Helper()
	mise, err := os.ReadFile("../../mise.toml")
	if err != nil {
		t.Fatal(err)
	}
	pin := regexp.MustCompile(`(?m)^pnpm = "([^"]+)"`).FindStringSubmatch(string(mise))
	if pin == nil {
		t.Fatal(`mise.toml has no 'pnpm = "<version>"' line`)
	}
	return pin[1]
}

// TestRenovateReadsTheDockerfilePnpm proves a custom regex manager of
// renovate.json matches the Dockerfile's PNPM_VERSION argument and extracts
// the npm datasource, the depName pnpm and the version mise.toml pins, so
// Renovate updates the Dockerfile in the same run as mise.toml. Renovate's
// regex syntax for named groups, (?<name>...), is also Go's.
func TestRenovateReadsTheDockerfilePnpm(t *testing.T) {
	t.Parallel()

	docker, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	pin := misePnpm(t)

	found := false
	for _, m := range readRenovateConfig(t).CustomManagers {
		if m.CustomType != "regex" || !slices.Contains(m.ManagerFilePatterns, "/^Dockerfile$/") {
			continue
		}
		for _, s := range m.MatchStrings {
			// The Dockerfile has more than one regex manager, this test is about the pnpm one.
			if !strings.Contains(s, "PNPM_VERSION") {
				continue
			}
			re, err := regexp.Compile(s)
			if err != nil {
				t.Fatalf("matchString %q does not compile: %v", s, err)
			}
			got := re.FindStringSubmatch(string(docker))
			if got == nil {
				continue
			}
			named := map[string]string{}
			for i, name := range re.SubexpNames() {
				if name != "" {
					named[name] = got[i]
				}
			}
			if named["datasource"] != "npm" || named["depName"] != "pnpm" || named["currentValue"] != pin {
				t.Errorf("the Dockerfile manager extracts %v, want datasource npm, depName pnpm, version %s",
					named, pin)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no regex manager of renovate.json matches the Dockerfile's PNPM_VERSION argument")
	}
}

// TestRenovateGroupsEveryPnpmPin proves one package rule puts the three places
// that name the pnpm version into one pull request: the mise.toml pin (mise
// manager), the Dockerfile argument (regex manager) and the govue rule's
// constraints.pnpm, which Renovate reads itself as a tool constraint
// (renovate-config manager). It also checks that the constraint equals the
// mise.toml pin today.
func TestRenovateGroupsEveryPnpmPin(t *testing.T) {
	t.Parallel()

	cfg := readRenovateConfig(t)
	pin := misePnpm(t)

	for _, r := range cfg.PackageRules {
		if c, ok := r.Constraints["pnpm"]; ok && c != pin {
			t.Errorf("constraints.pnpm is %s, mise.toml pins %s: they move together", c, pin)
		}
	}
	for _, r := range cfg.PackageRules {
		if !slices.Contains(r.MatchPackageNames, "pnpm") {
			continue
		}
		if strings.TrimSpace(r.GroupName) == "" {
			t.Fatal("the pnpm rule has no groupName: the pins would move in separate pull requests")
		}
		for _, manager := range []string{"mise", "custom.regex", "renovate-config"} {
			if !slices.Contains(r.MatchManagers, manager) {
				t.Errorf("the pnpm rule does not match the %s manager", manager)
			}
		}
		return
	}
	t.Fatal("renovate.json has no package rule for pnpm")
}
