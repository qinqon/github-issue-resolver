package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func piFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "pi-"+name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestParsePiResult covers settled answers, protocol failures, and cost accounting
// across authoritative messages, repeated representations, retries, and compaction.
func TestParsePiResult(t *testing.T) {
	success := string(piFixture(t, "success"))
	type testCase struct {
		name    string
		stdout  string
		text    string
		cost    float64
		wantErr string
	}
	tests := []testCase{
		{name: "success", stdout: success, text: "Fixed the bug.\nTests pass.", cost: 0.125},
		{name: "blank lines", stdout: " \n\t\n" + success + "\n", text: "Fixed the bug.\nTests pass.", cost: 0.125},
		{name: "no trailing newline", stdout: strings.TrimSuffix(success, "\n"), text: "Fixed the bug.\nTests pass.", cost: 0.125},
		{name: "multi turn repeated representations", stdout: string(piFixture(t, "multi-turn")), text: "Fixed the bug.\nTests pass.", cost: 0.375},
		{name: "successful retry", stdout: string(piFixture(t, "retry")), text: "Recovered.", cost: 0.375},
		{name: "cumulative updates replaced not added", stdout: string(piFixture(t, "updates")), text: "Done.", cost: 0.375},
		{name: "unfinished retry retains each attempt", stdout: string(piFixture(t, "partial-cost")), cost: 0.625, wantErr: "pi did not complete"},
		{name: "malformed JSON salvages later costs", stdout: string(piFixture(t, "malformed")), cost: 0.25, wantErr: "invalid or truncated Pi JSON event"},
		{name: "compaction usage", stdout: string(piFixture(t, "compaction")), text: "Done after compaction.", cost: 0.5},
		{name: "late error supersedes answer", stdout: string(piFixture(t, "late-error")), cost: 0.375, wantErr: `stopReason="error" retry exhausted`},
		{name: "empty", wantErr: "pi did not complete"},
		{name: "malformed only", stdout: "not JSON\n", wantErr: "invalid or truncated Pi JSON event"},
		{name: "truncated after success", stdout: success + `{"type":`, cost: 0.125, wantErr: "invalid or truncated Pi JSON event"},
		{name: "missing settled", stdout: strings.ReplaceAll(success, `{"type":"agent_settled"}`, ""), cost: 0.125, wantErr: "missing final agent_settled"},
		{name: "settled without assistant", stdout: `{"type":"agent_settled"}`, wantErr: "pi did not complete"},
		{name: "error", stdout: strings.ReplaceAll(success, `"stopReason":"stop"`, `"stopReason":"error","errorMessage":"provider failed"`), cost: 0.125, wantErr: `stopReason="error" provider failed`},
		{name: "aborted", stdout: strings.ReplaceAll(success, `"stopReason":"stop"`, `"stopReason":"aborted","errorMessage":"user cancelled"`), cost: 0.125, wantErr: `stopReason="aborted" user cancelled`},
		{name: "length", stdout: strings.ReplaceAll(success, `"stopReason":"stop"`, `"stopReason":"length"`), cost: 0.125, wantErr: `stopReason="length"`},
		{name: "tool use is not final", stdout: strings.ReplaceAll(success, `"stopReason":"stop"`, `"stopReason":"toolUse"`), cost: 0.125, wantErr: `stopReason="toolUse"`},
		{name: "reasoning and tools only", stdout: strings.ReplaceAll(success, `"type":"text"`, `"type":"thinking"`), cost: 0.125, wantErr: "without a final assistant answer"},
		{name: "whitespace answer", stdout: "{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\" \\n\\t\"}],\"stopReason\":\"stop\"}}\n{\"type\":\"agent_settled\"}", wantErr: "without a final assistant answer"},
		{name: "negative cost ignored", stdout: strings.ReplaceAll(success, `"total":0.125`, `"total":-1`), text: "Fixed the bug.\nTests pass."},
		{name: "compaction aborted retains cost", stdout: success + "{\"type\":\"compaction_start\"}\n{\"type\":\"compaction_end\",\"aborted\":true,\"result\":{\"usage\":{\"cost\":{\"total\":0.25}}}}\n", cost: 0.375, wantErr: "pi compaction failed (aborted=true)"},
		{name: "compaction error retains cost", stdout: success + "{\"type\":\"compaction_start\"}\n{\"type\":\"compaction_end\",\"errorMessage\":\"summary failed\",\"result\":{\"usage\":{\"cost\":{\"total\":0.25}}}}\n", cost: 0.375, wantErr: "summary failed"},
		{name: "failed preflight compaction recovers", stdout: "{\"type\":\"compaction_start\"}\n{\"type\":\"compaction_end\",\"errorMessage\":\"summary failed\"}\n" + success, text: "Fixed the bug.\nTests pass.", cost: 0.125},
		{name: "failed post-answer compaction still settles", stdout: success + "{\"type\":\"compaction_start\"}\n{\"type\":\"compaction_end\",\"errorMessage\":\"summary failed\"}\n{\"type\":\"agent_settled\"}\n", text: "Fixed the bug.\nTests pass.", cost: 0.125},
		{name: "aborted post-answer compaction rejects settlement", stdout: success + "{\"type\":\"compaction_start\"}\n{\"type\":\"compaction_end\",\"aborted\":true}\n{\"type\":\"agent_settled\"}\n", cost: 0.125, wantErr: "aborted=true"},
	}
	for _, event := range []string{"agent_start", "turn_start", "auto_retry_start", "compaction_start", "message_update"} {
		tests = append(tests, testCase{name: "new activity invalidates settled/" + event, stdout: success + fmt.Sprintf("{\"type\":%q}\n", event), cost: 0.125, wantErr: "missing final agent_settled"})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePiResult([]byte(tt.stdout))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("parsePiResult: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want containing %q", err, tt.wantErr)
			}
			if result.Result != tt.text {
				t.Errorf("answer = %q, want %q", result.Result, tt.text)
			}
			if math.Abs(result.CostUSD-tt.cost) > 1e-12 {
				t.Errorf("cost = %.12f, want %.12f", result.CostUSD, tt.cost)
			}
		})
	}
}

// TestPiAgentInvocation pins the full argv and stdin contract, including resume
// with an empty session namespace and canonicalization before hashing the cwd.
func TestPiAgentInvocation(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		resume bool
	}{
		{name: "fresh default model"},
		{name: "fresh explicit model", model: "anthropic/claude-sonnet-4"},
		{name: "resume missing session", resume: true},
		{name: "resume explicit model", model: "provider/model:thinking", resume: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", cache)
			workDir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "worktree alias")
			if err := os.Symlink(workDir, alias); err != nil {
				t.Fatal(err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(cwd, alias)
			if err != nil {
				t.Fatal(err)
			}
			prompt := "--model malicious\nquotes ' \" $HOME\n" + strings.Repeat("long prompt ", 30000)
			runner := &mockCommandRunner{stdout: piFixture(t, "success")}
			pi := &PiAgent{Model: tt.model}
			result, err := pi.Run(context.Background(), runner, relative, prompt, nil, tt.resume)
			if err != nil {
				t.Fatal(err)
			}
			if result.Result != "Fixed the bug.\nTests pass." || result.CostUSD != 0.125 {
				t.Errorf("result = %+v", result)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(runner.calls))
			}
			sessionDir := filepath.Join(cache, "oompa", "pi-sessions", fmt.Sprintf("%x", sha256.Sum256([]byte(workDir))))
			wantArgs := []string{"-p", "--mode", "json", "--session-dir", sessionDir,
				"--offline", "--no-approve", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files",
				"--system-prompt", "",
				"--append-system-prompt", piTaskPolicy}
			if tt.resume {
				wantArgs = append(wantArgs, "--continue")
			}
			if tt.model != "" {
				wantArgs = append(wantArgs, "--model", tt.model)
			}
			call := runner.calls[0]
			if call.Name != "pi" || call.WorkDir != workDir || call.Stdin != prompt || !reflect.DeepEqual(call.Args, wantArgs) {
				t.Errorf("invocation mismatch: binary=%q cwd=%q argv=%q stdin matches=%t", call.Name, call.WorkDir, call.Args, call.Stdin == prompt)
			}
			info, err := os.Stat(sessionDir)
			if err != nil {
				t.Fatal(err)
			}
			if !info.IsDir() || info.Mode().Perm() != 0o700 {
				t.Errorf("session directory mode = %v, want drwx------", info.Mode())
			}
			entries, err := os.ReadDir(sessionDir)
			if err != nil || len(entries) != 0 {
				t.Errorf("missing session should be delegated to Pi, entries=%v err=%v", entries, err)
			}
		})
	}
}

// TestPiAgentSessionIsolation ensures stable sessions across aliases and fresh
// agent instances, without sharing another worktree's or backend's namespace.
func TestPiAgentSessionIsolation(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	first, second := t.TempDir(), t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatal(err)
	}
	runner := &mockCommandRunner{stdout: piFixture(t, "success")}
	for _, dir := range []string{first, second, alias, first} {
		if _, err := (&PiAgent{}).Run(context.Background(), runner, dir, "continue", nil, true); err != nil {
			t.Fatal(err)
		}
	}
	sessions := []string{runner.calls[0].Args[4], runner.calls[1].Args[4], runner.calls[2].Args[4], runner.calls[3].Args[4]}
	if sessions[0] == sessions[1] || sessions[0] != sessions[2] || sessions[0] != sessions[3] {
		t.Errorf("session isolation/stability violated: %q", sessions)
	}
	for _, backend := range []CodeAgent{&ClaudeCodeAgent{}, &OpenCodeAgent{}} {
		other := &mockCommandRunner{stdout: append(streamResultJSON(AgentResult{Result: "ok"}), []byte("{\"type\":\"step_finish\",\"part\":{\"reason\":\"stop\"}}\n")...)}
		if _, err := backend.Run(context.Background(), other, first, "continue", nil, true); err != nil {
			t.Fatal(err)
		}
		for _, arg := range other.calls[0].Args {
			if strings.Contains(arg, "pi-sessions") || arg == "--session-dir" {
				t.Errorf("%s used Pi session namespace: %q", other.calls[0].Name, other.calls[0].Args)
			}
		}
	}
}

// TestPiAgentSessionPermissions rejects unsafe namespaces before invocation,
// preserving existing history and leaving operator cache permissions alone.
func TestPiAgentSessionPermissions(t *testing.T) {
	for _, level := range []string{"cache", "oompa", "pi-sessions", "worktree"} {
		for _, condition := range []string{"private", "readable", "writable", "symlink", "foreign owner"} {
			t.Run(level+"/"+condition, func(t *testing.T) {
				if condition == "foreign owner" && os.Geteuid() != 0 {
					t.Skip("changing directory ownership requires root")
				}
				cache := t.TempDir()
				t.Setenv("XDG_CACHE_HOME", cache)
				if runtime.GOOS == "darwin" {
					t.Setenv("HOME", t.TempDir())
					cache = filepath.Join(os.Getenv("HOME"), "Library", "Caches")
				}
				workDir, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				sessionDir := filepath.Join(cache, "oompa", "pi-sessions", fmt.Sprintf("%x", sha256.Sum256([]byte(workDir))))
				if err := os.MkdirAll(sessionDir, 0o700); err != nil {
					t.Fatal(err)
				}
				dataPath := filepath.Join(sessionDir, "existing.jsonl")
				const history = "existing coding session\n"
				if err := os.WriteFile(dataPath, []byte(history), 0o600); err != nil {
					t.Fatal(err)
				}
				dir := map[string]string{
					"cache": cache, "oompa": filepath.Join(cache, "oompa"),
					"pi-sessions": filepath.Dir(sessionDir), "worktree": sessionDir,
				}[level]
				wantMode := os.FileMode(0o700)
				wantUID := uint32(os.Geteuid())
				switch condition {
				case "readable":
					wantMode = 0o755
				case "writable":
					wantMode = 0o722
				case "symlink":
					rel, err := filepath.Rel(dir, dataPath)
					if err != nil {
						t.Fatal(err)
					}
					target := filepath.Join(t.TempDir(), "target")
					if err := os.Rename(dir, target); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, dir); err != nil {
						t.Fatal(err)
					}
					dataPath = filepath.Join(target, rel)
					dir = target
				case "foreign owner":
					wantUID = 1
					if err := os.Chown(dir, int(wantUID), -1); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Chmod(dir, wantMode); err != nil {
					t.Fatal(err)
				}
				cacheBefore, err := os.Stat(cache)
				if err != nil {
					t.Fatal(err)
				}
				wantOK := condition == "private" || level == "cache" && (condition == "readable" || condition == "symlink")
				for _, resume := range []bool{false, true} {
					runner := &mockCommandRunner{stdout: piFixture(t, "success")}
					result, err := (&PiAgent{}).Run(context.Background(), runner, workDir, "task", nil, resume)
					if wantOK {
						if err != nil || len(runner.calls) != 1 {
							t.Fatalf("secure namespace rejected: err=%v calls=%v", err, runner.calls)
						}
						if slices.Contains(runner.calls[0].Args, "--continue") != resume {
							t.Errorf("resume=%t: args=%q", resume, runner.calls[0].Args)
						}
					} else if err == nil || !strings.Contains(err.Error(), "pi session directory") || len(runner.calls) != 0 || result != (AgentResult{}) {
						t.Fatalf("unsafe namespace not rejected: err=%v calls=%v result=%+v", err, runner.calls, result)
					}
				}
				data, err := os.ReadFile(dataPath)
				if err != nil || string(data) != history {
					t.Errorf("session data changed: data=%q err=%v", data, err)
				}
				info, err := os.Stat(dir)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != wantMode || info.Sys().(*syscall.Stat_t).Uid != wantUID {
					t.Errorf("existing directory permissions or ownership changed: %+v", info)
				}
				cacheAfter, err := os.Stat(cache)
				if err != nil {
					t.Fatal(err)
				}
				if cacheAfter.Mode() != cacheBefore.Mode() {
					t.Errorf("operator cache mode changed: %v -> %v", cacheBefore.Mode(), cacheAfter.Mode())
				}
			})
		}
	}
}

// Auxiliary calls must never supersede the session selected for continued coding.
func TestPiAgentAuxiliarySessions(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, prompt := range []string{
		buildChangeSummaryPrompt("diff"),
		buildFlakyMatchPrompt("test", "failure", nil),
		buildTriageMatchPrompt("test", "failure", nil, nil),
	} {
		for _, resume := range []bool{false, true} {
			runner := &mockCommandRunner{stdout: piFixture(t, "success")}
			if _, err := (&PiAgent{}).Run(context.Background(), runner, t.TempDir(), prompt, nil, resume); err != nil {
				t.Fatal(err)
			}
			args := runner.calls[0].Args
			if !slices.Contains(args, "--no-session") || slices.Contains(args, "--continue") {
				t.Errorf("auxiliary invocation must be ephemeral: %q", args)
			}
		}
	}
}

// TestPiAgentFailures verifies shared-runner errors preserve their cause and
// stderr, salvage all available cost, and never expose a failed answer.
func TestPiAgentFailures(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	workDir := t.TempDir()
	exitErr := errors.New("exit status 7")
	tests := []struct {
		name    string
		fixture string
		runErr  error
		cost    float64
		wantErr string
	}{
		{name: "failed after final answer", fixture: "success", runErr: exitErr, cost: 0.125, wantErr: "pi invocation failed"},
		{name: "failed during retry", fixture: "partial-cost", runErr: exitErr, cost: 0.625, wantErr: "pi invocation failed"},
		{name: "failed malformed output", fixture: "malformed", runErr: exitErr, cost: 0.25, wantErr: "pi invocation failed"},
		{name: "missing executable", runErr: os.ErrNotExist, wantErr: "pi invocation failed"},
		{name: "zero exit protocol error", fixture: "late-error", cost: 0.375, wantErr: "retry exhausted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &mockCommandRunner{err: tt.runErr, stderr: []byte("diagnostic stderr")}
			if tt.fixture != "" {
				runner.stdout = piFixture(t, tt.fixture)
			}
			result, err := (&PiAgent{}).Run(context.Background(), runner, workDir, "task", discardLogger(), false)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if tt.runErr != nil && (!errors.Is(err, tt.runErr) || !strings.Contains(err.Error(), "diagnostic stderr")) {
				t.Errorf("invocation lost cause or stderr: %v", err)
			}
			if result.Result != "" || result.CostUSD != tt.cost {
				t.Errorf("result = %+v, want cost-only %g", result, tt.cost)
			}
		})
	}
}

// TestPiAgentPreflightFailures ensures invalid local prerequisites fail before
// invoking the model, including when a required skill cannot be prepared.
func TestPiAgentPreflightFailures(t *testing.T) {
	for _, name := range []string{"missing worktree", "blocked session directory", "missing skill"} {
		t.Run(name, func(t *testing.T) {
			cache, workDir := t.TempDir(), t.TempDir()
			t.Setenv("XDG_CACHE_HOME", cache)
			t.Setenv("OOMPA_CE_DIR", t.TempDir())
			prompt, wantErr := "task", "pi worktree"
			switch name {
			case "missing worktree":
				workDir = filepath.Join(workDir, "missing")
			case "blocked session directory":
				if err := os.WriteFile(filepath.Join(cache, "oompa"), []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				wantErr = "pi session directory"
			case "missing skill":
				prompt, wantErr = "You are resolving GitHub issue #42.", "pi CE resource"
			}
			runner := &mockCommandRunner{}
			result, err := (&PiAgent{}).Run(context.Background(), runner, workDir, prompt, nil, false)
			if err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Errorf("error = %v, want %q", err, wantErr)
			}
			if len(runner.calls) != 0 || result != (AgentResult{}) {
				t.Errorf("preflight invoked runner or returned data: calls=%v result=%+v", runner.calls, result)
			}
		})
	}
}

// Pi's unconditional legacy-directory migration must not mutate target projects.
func TestPiAgentRejectsProjectMigration(t *testing.T) {
	for _, tt := range []struct {
		name     string
		symlink  bool
		dangling bool
		migrated bool
	}{
		{name: "directory"},
		{name: "symlink", symlink: true},
		{name: "destination exists", migrated: true},
		{name: "symlink with destination exists", symlink: true, migrated: true},
		{name: "dangling symlink", symlink: true, dangling: true},
		{name: "dangling symlink with destination exists", symlink: true, dangling: true, migrated: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			workDir := t.TempDir()
			commands := filepath.Join(workDir, ".pi", "commands")
			if err := os.MkdirAll(filepath.Dir(commands), 0o700); err != nil {
				t.Fatal(err)
			}
			if tt.symlink {
				target := t.TempDir()
				if tt.dangling {
					target = filepath.Join(target, "missing")
				}
				if err := os.Symlink(target, commands); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(commands, 0o700); err != nil {
				t.Fatal(err)
			}
			if tt.migrated {
				if err := os.Mkdir(filepath.Join(workDir, ".pi", "prompts"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			runner := &mockCommandRunner{stdout: piFixture(t, "success")}
			_, err := (&PiAgent{}).Run(context.Background(), runner, workDir, buildChangeSummaryPrompt("diff"), nil, false)
			if tt.migrated && !tt.symlink {
				if err != nil || len(runner.calls) != 1 {
					t.Fatalf("migrated project rejected: %v", err)
				}
			} else {
				wantError := "would migrate"
				if tt.symlink {
					wantError = "refuses symlinked commands path"
				}
				if err == nil || !strings.Contains(err.Error(), wantError) || len(runner.calls) != 0 {
					t.Fatalf("legacy project not rejected before invocation: %v, calls=%v", err, runner.calls)
				}
			}
			if _, err := os.Lstat(commands); err != nil {
				t.Fatalf("preflight changed project: %v", err)
			}
		})
	}
}

// TestPiAgentTimeoutAndCancellation exercises the real shared ExecRunner in
// buffered and streaming modes, without installing Pi or calling a provider.
func TestPiAgentTimeoutAndCancellation(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, timeout := range []bool{false, true} {
			t.Run(fmt.Sprintf("streaming=%t/timeout=%t", streaming, timeout), func(t *testing.T) {
				bin, workDir := t.TempDir(), t.TempDir()
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				ready := filepath.Join(workDir, "ready")
				script := "#!/bin/sh\nprintf '%s\\n' \"$PI_TEST_OUTPUT\"\nprintf 'started\\n' >&2\nprintf ready > \"$PI_TEST_READY\"\nexec sleep 30\n"
				if err := os.WriteFile(filepath.Join(bin, "pi"), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
				runner := &ExecRunner{Env: []string{"PI_TEST_OUTPUT=" + string(piFixture(t, "partial-cost")), "PI_TEST_READY=" + ready}}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				pi := &PiAgent{}
				if timeout {
					pi.Timeout = 2 * time.Second
				} else {
					finished := make(chan struct{})
					defer func() { cancel(); <-finished }()
					go func() {
						defer close(finished)
						ticker := time.NewTicker(5 * time.Millisecond)
						defer ticker.Stop()
						for {
							select {
							case <-ctx.Done():
								return
							case <-ticker.C:
								if _, err := os.Stat(ready); err == nil {
									cancel()
									return
								}
							}
						}
					}()
				}
				var logger *slog.Logger
				if streaming {
					logger = discardLogger()
				}
				start := time.Now()
				result, err := pi.Run(ctx, runner, workDir, "task", logger, false)
				if err == nil || !strings.Contains(err.Error(), "pi invocation failed") || !strings.Contains(err.Error(), "started") {
					t.Errorf("error = %v, want invocation failure with subprocess stderr", err)
				}
				if result.Result != "" || result.CostUSD != 0.625 {
					t.Errorf("result = %+v, want cost-only 0.625", result)
				}
				if elapsed := time.Since(start); elapsed > 8*time.Second {
					t.Errorf("subprocess did not stop promptly: %s", elapsed)
				}
				if timeout && ctx.Err() != nil {
					t.Errorf("parent deadline fired instead of Pi timeout: %v", ctx.Err())
				}
				if !timeout && !errors.Is(ctx.Err(), context.Canceled) {
					t.Errorf("parent context = %v, want cancellation", ctx.Err())
				}
			})
		}
	}
}
