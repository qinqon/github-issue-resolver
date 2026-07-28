package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// runCLIAgent timeout behavior: when a per-invocation timeout is set, a
// hung child process must not block the goroutine forever (#289).

func TestRunCLIAgent_TimeoutKillsHungInvocation(t *testing.T) {
	// Use a slow mock runner that blocks until the context is cancelled.
	runner := &slowMockRunner{delay: 10 * time.Second}

	spec := cliAgentSpec{
		binary:  "test-agent",
		args:    nil,
		logLine: nil,
		parse:   func(stdout []byte) (AgentResult, error) { return AgentResult{}, nil },
		timeout: 100 * time.Millisecond,
	}

	start := time.Now()
	_, err := runCLIAgent(context.Background(), runner, "/tmp", "prompt", discardLogger(), spec)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from timed-out invocation")
	}
	if !strings.Contains(err.Error(), "test-agent invocation failed") {
		t.Errorf("expected agent invocation error, got: %v", err)
	}
	// Should complete well under the 10s delay — the timeout fires at 100ms.
	if elapsed > 2*time.Second {
		t.Errorf("expected completion near 100ms timeout, took %v", elapsed)
	}
}

func TestRunCLIAgent_NoTimeoutPassesContextThrough(t *testing.T) {
	// With timeout=0, the parent context controls the deadline.
	runner := &mockCommandRunner{
		stdout: streamResultJSON(AgentResult{Result: "ok", CostUSD: 0.01}),
	}

	spec := cliAgentSpec{
		binary:  "claude",
		args:    []string{"-p"},
		logLine: nil,
		parse:   parseStreamResult,
		timeout: 0, // unlimited
	}

	result, err := runCLIAgent(context.Background(), runner, "/tmp", "prompt", discardLogger(), spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result != "ok" {
		t.Errorf("expected result 'ok', got %q", result.Result)
	}
}

func TestRunCLIAgent_NilParserReturnsError(t *testing.T) {
	spec := cliAgentSpec{
		binary: "test-agent",
		parse:  nil,
	}

	_, err := runCLIAgent(context.Background(), &mockCommandRunner{}, "/tmp", "prompt", nil, spec)
	if err == nil {
		t.Fatal("expected error for nil parser")
	}
	if !strings.Contains(err.Error(), "has no parser") {
		t.Errorf("expected parser error, got: %v", err)
	}
}

func TestRunCLIAgent_TimeoutSalvagesCost(t *testing.T) {
	// When timeout fires but partial output was emitted, cost should be salvaged.
	partial := streamResultJSON(AgentResult{Result: "partial", CostUSD: 0.50})

	runner := &slowMockRunner{
		delay:  10 * time.Second,
		stdout: partial,
	}

	spec := cliAgentSpec{
		binary:  "claude",
		args:    nil,
		logLine: nil,
		parse:   parseStreamResult,
		timeout: 100 * time.Millisecond,
	}

	result, err := runCLIAgent(context.Background(), runner, "/tmp", "prompt", discardLogger(), spec)
	if err == nil {
		t.Fatal("expected error from timed-out invocation")
	}
	if result.CostUSD != 0.50 {
		t.Errorf("expected salvaged cost 0.50, got %f", result.CostUSD)
	}
}

// slowMockRunner blocks for delay (or until ctx is cancelled) and then
// returns the configured stdout/stderr/err. This simulates a hung child
// process for timeout testing.
type slowMockRunner struct {
	delay  time.Duration
	stdout []byte
	stderr []byte
	err    error
}

func (m *slowMockRunner) Run(ctx context.Context, _, _ string, _ ...string) (stdout, stderr []byte, err error) {
	return m.RunWithStdin(ctx, "", "", "")
}

func (m *slowMockRunner) RunWithStdin(ctx context.Context, _, _, _ string, _ ...string) (stdout, stderr []byte, err error) {
	select {
	case <-time.After(m.delay):
		return m.stdout, m.stderr, m.err
	case <-ctx.Done():
		return m.stdout, m.stderr, ctx.Err()
	}
}
