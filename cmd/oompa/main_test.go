package main

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qinqon/oompa/pkg/agent"
)

// TestParseConfig_AgentSelection covers the default, environment, and CLI overrides.
func TestParseConfig_AgentSelection(t *testing.T) {
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if strings.HasPrefix(key, "OOMPA_") || strings.HasPrefix(key, "GITHUB_APP_") {
			t.Setenv(key, "")
		}
	}
	tests := []struct {
		name      string
		envAgent  string
		envModel  string
		args      []string
		wantAgent string
		wantModel string
	}{
		{name: "OpenCode remains default", wantAgent: "opencode"},
		{name: "Pi environment", envAgent: "pi", envModel: "env-model", wantAgent: "pi", wantModel: "env-model"},
		{name: "Pi flags override environment", envAgent: "opencode", envModel: "env-model", args: []string{"--agent", "pi", "--agent-model", "cli-model"}, wantAgent: "pi", wantModel: "cli-model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OOMPA_AGENT", tt.envAgent)
			t.Setenv("OOMPA_AGENT_MODEL", tt.envModel)
			oldFlags, oldArgs := flag.CommandLine, os.Args
			t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
			flag.CommandLine = flag.NewFlagSet("oompa", flag.ContinueOnError)
			os.Args = append([]string{"oompa", "--repo", "org/repo"}, tt.args...)
			cfg, _, _ := parseConfig()
			if cfg.Agent != tt.wantAgent || cfg.AgentModel != tt.wantModel || cfg.AgentTimeout != 30*time.Minute {
				t.Fatalf("got backend/model/timeout %s/%s/%s, want %s/%s/30m", cfg.Agent, cfg.AgentModel, cfg.AgentTimeout, tt.wantAgent, tt.wantModel)
			}
		})
	}
}

// TestSelectCodeAgent verifies backend construction and unsupported combinations.
func TestSelectCodeAgent(t *testing.T) {
	tests := []struct {
		name    string
		cfg     agent.Config
		want    agent.CodeAgent
		wantErr string
	}{
		{"Pi model and timeout", agent.Config{Agent: "pi", AgentModel: "provider/model", AgentTimeout: time.Minute}, &agent.PiAgent{Model: "provider/model", Timeout: time.Minute}, ""},
		{"Pi defaults", agent.Config{Agent: "pi"}, &agent.PiAgent{}, ""},
		{"OpenCode", agent.Config{Agent: "opencode", AgentModel: "model", AgentTimeout: time.Minute}, &agent.OpenCodeAgent{Model: "model", Timeout: time.Minute}, ""},
		{"Claude Code", agent.Config{Agent: "claudecode", AgentTimeout: time.Minute}, &agent.ClaudeCodeAgent{Timeout: time.Minute}, ""},
		{"Claude model rejected", agent.Config{Agent: "claudecode", AgentModel: "model"}, nil, "agent-model"},
		{"unknown backend", agent.Config{Agent: "unknown"}, nil, "invalid agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectCodeAgent(tt.cfg)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || got != nil {
					t.Fatalf("got (%v, %v), want nil and error containing %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got (%#v, %v), want %#v", got, err, tt.want)
			}
		})
	}
}

// TestValidateAgentConfigs_Resolved checks resources only for effective YAML backends
// and rejects inherited model incompatibilities before any worker starts.
func TestValidateAgentConfigs_Resolved(t *testing.T) {
	tests := []struct {
		name    string
		global  agent.Config
		yaml    string
		wantErr string
	}{
		{"YAML Pi overrides OpenCode default", agent.Config{Agent: "opencode"}, "agent: pi\n", "pi backend resources"},
		{"YAML OpenCode overrides Pi", agent.Config{Agent: "pi"}, "agent: opencode\n", "opencode backend resources"},
		{"YAML Claude skips OpenCode resources", agent.Config{Agent: "opencode"}, "agent: claudecode\n", ""},
		{"inherited Pi requires CE checkout", agent.Config{Agent: "pi"}, "", "pi backend resources"},
		{"inherited OpenCode requires skills", agent.Config{Agent: "opencode"}, "", "opencode backend resources"},
		{"inherited Claude rejects YAML model", agent.Config{Agent: "claudecode"}, "agent-model: file-model\n", "agent-model"},
		{"YAML Claude rejects inherited model", agent.Config{Agent: "opencode", AgentModel: "cli-model"}, "agent: claudecode\n", "agent-model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("OPENCODE_CONFIG_DIR", "")
			t.Setenv("OOMPA_CE_DIR", "")
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml+"projects:\n  - repo: org/repo\n    issues: [{}]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			fc, err := agent.LoadFileConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			entries := agent.BuildRoleEntries(fc, t.TempDir(), tt.global)
			if len(entries) != 1 {
				t.Fatalf("expected one resolved entry, got %d", len(entries))
			}
			err = validateAgentConfigs(entries[0].Config)
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

func TestValidateAgentConfigs_ValidatesAllModelsBeforeResources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	err := validateAgentConfigs(
		agent.Config{Agent: "opencode"},
		agent.Config{Agent: "claudecode", AgentModel: "model", Owner: "org", Repo: "repo", Role: "issues"},
	)
	if err == nil || !strings.Contains(err.Error(), "org/repo (issues): agent-model") {
		t.Fatalf("expected resolved model error before resource checks, got %v", err)
	}
}
