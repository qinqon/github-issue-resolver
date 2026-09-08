package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadPluginVersionFromFile_ValidPackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgFile, []byte(`{"name":"@opencode-ai/plugin","version":"1.15.13"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	version, err := readPluginVersionFromFile(pkgFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.15.13" {
		t.Errorf("expected version 1.15.13, got %s", version)
	}
}

func TestReadPluginVersionFromFile_MissingFile(t *testing.T) {
	_, err := readPluginVersionFromFile("/nonexistent/package.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadPluginVersionFromFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgFile, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := readPluginVersionFromFile(pkgFile)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadPluginVersionFromFile_NoVersionField(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgFile, []byte(`{"name":"@opencode-ai/plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := readPluginVersionFromFile(pkgFile)
	if err == nil {
		t.Fatal("expected error for missing version field")
	}
	if !strings.Contains(err.Error(), "no version field") {
		t.Errorf("expected 'no version field' in error, got: %v", err)
	}
}

func TestReadPluginVersionFromFile_EmptyVersion(t *testing.T) {
	dir := t.TempDir()
	pkgFile := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkgFile, []byte(`{"name":"@opencode-ai/plugin","version":""}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := readPluginVersionFromFile(pkgFile)
	if err == nil {
		t.Fatal("expected error for empty version")
	}
}

// TestRequirePluginInstalled preserves the shipped SDK version-check API.
func TestRequirePluginInstalled(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "installed SDK", version: "1.15.13"},
		{name: "missing SDK"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			path := filepath.Join(home, ".config", "opencode", "node_modules", "@opencode-ai", "plugin", "package.json")
			if tt.version != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"name":"@opencode-ai/plugin","version":"1.15.13"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			version, err := RequirePluginInstalled()
			if tt.version == "" {
				if err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("expected error containing missing path %q, got %v", path, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if version != tt.version {
				t.Errorf("expected version %q, got %q", tt.version, version)
			}
		})
	}
}

// TestRequireOpenCodeSkills checks actual prompt resources, not SDK installation.
func TestRequireOpenCodeSkills(t *testing.T) {
	tests := []struct {
		name       string
		dirs       []string
		badSkill   string
		badContent string
		directory  bool
		sdkOnly    bool
		wantErr    string
	}{
		{name: "plural", dirs: []string{"skills"}},
		{name: "legacy singular", dirs: []string{"skill"}},
		{name: "mixed locations", dirs: []string{"skills", "skill"}},
		{name: "missing", wantErr: "ce-commit/SKILL.md"},
		{name: "SDK is insufficient", sdkOnly: true, wantErr: "ce-commit/SKILL.md"},
		{name: "empty skill", dirs: []string{"skills"}, badSkill: "ce-debug", wantErr: "ce-debug/SKILL.md"},
		{name: "blank skill", dirs: []string{"skills"}, badSkill: "ce-resolve-pr-feedback", badContent: " \n", wantErr: "ce-resolve-pr-feedback/SKILL.md"},
		{name: "directory is not a skill", dirs: []string{"skills"}, badSkill: "ce-debug", directory: true, wantErr: "ce-debug/SKILL.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("OPENCODE_CONFIG_DIR", "")
			if tt.sdkOnly {
				dir := filepath.Join(home, ".config", "opencode", "node_modules", "@opencode-ai", "plugin")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"version":"1.15.13"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if len(tt.dirs) > 0 {
				for i, skill := range []string{"ce-commit", "ce-debug", "ce-resolve-pr-feedback"} {
					path := filepath.Join(home, ".config", "opencode", tt.dirs[i%len(tt.dirs)], skill, "SKILL.md")
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					content := "# " + skill
					if skill == tt.badSkill {
						content = tt.badContent
						if tt.directory {
							if err := os.Mkdir(path, 0o755); err != nil {
								t.Fatal(err)
							}
							continue
						}
					}
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			err := RequireOpenCodeSkills()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

// TestRequireOpenCodeSkillsConfigDir follows CE installer root resolution and
// never lets a default installation mask missing skills in an explicit root.
func TestRequireOpenCodeSkillsConfigDir(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(t.TempDir(), "custom config")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, custom)
	if err != nil {
		t.Fatal(err)
	}
	defaultRoot := filepath.Join(home, ".config", "opencode")
	tests := []struct {
		name     string
		envDir   string
		root     string
		legacy   bool
		noHome   bool
		badSkill bool
	}{
		{name: "empty uses default", root: defaultRoot},
		{name: "blank uses default", envDir: " \n\t", root: defaultRoot},
		{name: "custom plural", envDir: custom, root: custom},
		{name: "custom legacy", envDir: custom, root: custom, legacy: true},
		{name: "custom without HOME", envDir: custom, root: custom, noHome: true},
		{name: "trimmed custom", envDir: " \t" + custom + "\n", root: custom},
		{name: "relative custom", envDir: relative, root: custom},
		{name: "home expansion", envDir: "~/custom config", root: filepath.Join(home, "custom config")},
		{name: "native home expansion", envDir: "~" + string(filepath.Separator) + "custom config", root: filepath.Join(home, "custom config")},
		{name: "bare home expansion", envDir: "~", root: home},
		{name: "custom missing skill does not fall back", envDir: custom, root: custom, badSkill: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			if tt.noHome {
				t.Setenv("HOME", "")
			}
			t.Setenv("OPENCODE_CONFIG_DIR", tt.envDir)
			roots := []string{tt.root}
			if tt.badSkill {
				roots = append(roots, defaultRoot)
			}
			for _, root := range roots {
				dir := "skills"
				if root == tt.root && tt.legacy {
					dir = "skill"
				}
				for _, skill := range []string{"ce-commit", "ce-debug", "ce-resolve-pr-feedback"} {
					if root == tt.root && tt.badSkill && skill == "ce-debug" {
						continue
					}
					path := filepath.Join(root, dir, skill, "SKILL.md")
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("# "+skill), 0o644); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
							t.Error(err)
						}
					})
				}
			}
			err := RequireOpenCodeSkills()
			if tt.badSkill {
				for _, want := range []string{"ce-debug/SKILL.md", filepath.Join(tt.root, "skills"), filepath.Join(tt.root, "skill")} {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Errorf("error = %v, want %q", err, want)
					}
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckPluginVersion_BestEffortNpmUnavailable(t *testing.T) {
	// This test verifies that CheckPluginVersion handles a missing npm gracefully
	// by returning nil (best-effort). Also validates readInstalledPluginVersion with
	// a fake home directory.

	// Create fake home with installed plugin
	fakeHome := t.TempDir()
	pluginDir := filepath.Join(fakeHome, ".config", "opencode", "node_modules", "@opencode-ai", "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, "package.json"),
		[]byte(`{"name":"@opencode-ai/plugin","version":"1.14.22"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Override HOME so readInstalledPluginVersion finds our fake directory
	t.Setenv("HOME", fakeHome)

	logger := discardLogger()

	// We can't easily mock npm view in a unit test, so we test the components
	// directly and test CheckPluginVersion integration only when npm is available.
	// The unit tests below cover the individual functions.

	// Test readInstalledPluginVersion with the fake home
	version, err := readInstalledPluginVersion()
	if err != nil {
		t.Fatalf("unexpected error reading installed version: %v", err)
	}
	if version != "1.14.22" {
		t.Errorf("expected installed version 1.14.22, got %s", version)
	}

	// Test that CheckPluginVersion handles missing npm gracefully
	// (best-effort: returns nil on error)
	t.Setenv("PATH", "") // ensure npm is not found
	finding := CheckPluginVersion(context.Background(), logger)
	// Should return nil because npm view will fail
	if finding != nil {
		t.Errorf("expected nil finding when npm is not available, got: %+v", finding)
	}
}

func TestCheckPluginVersion_BestEffortPluginNotInstalled(t *testing.T) {
	// When the plugin is not installed (package.json missing), CheckPluginVersion
	// should silently return nil (best-effort).
	logger := discardLogger()

	t.Setenv("HOME", t.TempDir())
	finding := CheckPluginVersion(context.Background(), logger)
	if finding != nil {
		t.Errorf("expected nil finding when plugin not installed, got: %+v", finding)
	}
}

func TestCheckPluginVersion_OutdatedWithFakeNpm(t *testing.T) {
	// Deterministic test: creates a fake npm script that returns a fixed "latest"
	// version, and a fake installed package.json with a different version.
	// Verifies CheckPluginVersion returns a non-nil finding with correct fields.

	// Create fake home with installed plugin at version 1.14.22
	fakeHome := t.TempDir()
	pluginDir := filepath.Join(fakeHome, ".config", "opencode", "node_modules", "@opencode-ai", "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, "package.json"),
		[]byte(`{"name":"@opencode-ai/plugin","version":"1.14.22"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", fakeHome)

	// Create a fake npm executable that prints "1.15.13" for `npm view ... version`
	fakeBin := t.TempDir()
	npmScript := filepath.Join(fakeBin, "npm")
	if err := os.WriteFile(npmScript, []byte("#!/bin/sh\necho 1.15.13\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)

	logger := discardLogger()
	finding := CheckPluginVersion(context.Background(), logger)

	if finding == nil {
		t.Fatal("expected non-nil finding when versions differ")
	}
	if finding.Owner != "opencode-ai" {
		t.Errorf("expected Owner 'opencode-ai', got %s", finding.Owner)
	}
	if finding.Repo != "plugin" {
		t.Errorf("expected Repo 'plugin', got %s", finding.Repo)
	}
	if finding.Category != "plugin" {
		t.Errorf("expected category 'plugin', got %s", finding.Category)
	}
	if !strings.Contains(finding.Message, "1.14.22") {
		t.Error("expected message to contain installed version 1.14.22")
	}
	if !strings.Contains(finding.Message, "1.15.13") {
		t.Error("expected message to contain latest version 1.15.13")
	}
	if finding.DedupKey != "plugin-outdated:1.14.22:1.15.13" {
		t.Errorf("expected DedupKey 'plugin-outdated:1.14.22:1.15.13', got %s", finding.DedupKey)
	}
}

func TestCheckPluginVersion_SameVersionWithFakeNpm(t *testing.T) {
	// Deterministic test: when installed and latest versions match,
	// CheckPluginVersion should return nil (no finding).

	fakeHome := t.TempDir()
	pluginDir := filepath.Join(fakeHome, ".config", "opencode", "node_modules", "@opencode-ai", "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(pluginDir, "package.json"),
		[]byte(`{"name":"@opencode-ai/plugin","version":"1.15.13"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", fakeHome)

	// Fake npm returns the same version
	fakeBin := t.TempDir()
	npmScript := filepath.Join(fakeBin, "npm")
	if err := os.WriteFile(npmScript, []byte("#!/bin/sh\necho 1.15.13\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin)

	logger := discardLogger()
	finding := CheckPluginVersion(context.Background(), logger)

	if finding != nil {
		t.Errorf("expected nil finding when versions match, got: %+v", finding)
	}
}

func TestCheckPluginVersion_FindingFormat(t *testing.T) {
	// Verify the SlackFinding format directly
	finding := &SlackFinding{
		Owner:    "opencode-ai",
		Repo:     "plugin",
		PRTitle:  "Plugin Version Check",
		PRURL:    "https://www.npmjs.com/package/@opencode-ai/plugin",
		Category: "plugin",
		Message: "⚠️ compound-engineering plugin outdated: installed 1.14.22, latest 1.15.13. " +
			"Run: `cd ~/.config/opencode && npm install @opencode-ai/plugin@latest`",
		DedupKey: "plugin-outdated:1.14.22:1.15.13",
	}

	if finding.Category != "plugin" {
		t.Errorf("expected category 'plugin', got %s", finding.Category)
	}
	if finding.Owner != "opencode-ai" {
		t.Errorf("expected Owner 'opencode-ai', got %s", finding.Owner)
	}
	if finding.Repo != "plugin" {
		t.Errorf("expected Repo 'plugin', got %s", finding.Repo)
	}
	if finding.PRTitle != "Plugin Version Check" {
		t.Errorf("expected PRTitle 'Plugin Version Check', got %s", finding.PRTitle)
	}
	if finding.PRURL != "https://www.npmjs.com/package/@opencode-ai/plugin" {
		t.Errorf("expected PRURL to point to npm package, got %s", finding.PRURL)
	}
	if !strings.Contains(finding.Message, "1.14.22") {
		t.Error("expected message to contain installed version")
	}
	if !strings.Contains(finding.Message, "1.15.13") {
		t.Error("expected message to contain latest version")
	}
	if !strings.Contains(finding.Message, "npm install") {
		t.Error("expected message to contain update command")
	}
	if !strings.Contains(finding.DedupKey, "1.14.22") || !strings.Contains(finding.DedupKey, "1.15.13") {
		t.Error("expected DedupKey to contain both versions")
	}
}

func TestStartPluginVersionChecker_DisabledWithoutSlack(t *testing.T) {
	logger := discardLogger()

	// Should return nil and not panic with nil Slack reporter
	ch := StartPluginVersionChecker(context.Background(), nil, logger)
	if ch != nil {
		t.Error("expected nil channel when Slack reporter is nil")
	}

	// Should return nil and not panic with disabled Slack reporter
	reporter := NewSlackReporter("", "", logger)
	ch = StartPluginVersionChecker(context.Background(), reporter, logger)
	if ch != nil {
		t.Error("expected nil channel when Slack reporter is disabled")
	}
}

func TestStartPluginVersionChecker_RunsAndStops(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Set HOME to a temp dir so the installed version check fails gracefully
	t.Setenv("HOME", t.TempDir())
	// Ensure npm is not found so fetchLatestPluginVersion fails gracefully
	t.Setenv("PATH", "")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := discardLogger()
	reporter := NewSlackReporter(ts.URL, "", logger)

	ctx, cancel := context.WithCancel(context.Background())
	initDone := StartPluginVersionChecker(ctx, reporter, logger)

	if initDone == nil {
		t.Fatal("expected non-nil initDone channel when Slack is enabled")
	}

	// Wait for the initial check to complete
	select {
	case <-initDone:
		// Initial check completed
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial plugin check")
	}

	// Cancel should stop the goroutine
	cancel()

	// Give it time to stop
	time.Sleep(100 * time.Millisecond)
	// No assertion needed — we're verifying it doesn't panic or leak
}
