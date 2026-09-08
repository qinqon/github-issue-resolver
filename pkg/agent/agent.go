package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// CodeAgent abstracts the CLI coding agent (Claude Code, OpenCode, or Pi).
type CodeAgent interface {
	Run(ctx context.Context, runner CommandRunner, workDir, prompt string,
		logger *slog.Logger, resume bool) (AgentResult, error)
}

// cliAgentSpec describes how to invoke and parse one CLI coding agent backend.
// The invocation flow is identical for every backend (stream when possible,
// pipe the prompt via stdin, salvage cost on failure); only the binary, its
// arguments, the per-line log decoder, and the result parser differ.
type cliAgentSpec struct {
	binary  string
	args    []string
	logLine func(logger *slog.Logger, line []byte)
	parse   func(stdout []byte) (AgentResult, error)
	timeout time.Duration // per-invocation timeout; 0 = unlimited
}

// watchdogInterval is the interval at which the stall watchdog logs a
// warning while an agent invocation is in flight.
const watchdogInterval = 5 * time.Minute

// runCLIAgent invokes a CLI coding agent backend described by spec.
// The prompt is passed via stdin to avoid hitting the OS ARG_MAX limit for
// large prompts. Output is streamed line-by-line to the logger when the
// runner supports it. On invocation failure the partial output is still
// parsed so any cost it reports can be billed against session budgets
// alongside the error (cost-only: result text never propagates on failure).
//
// When spec.timeout > 0, a per-invocation deadline is applied so a hung
// child process cannot stall the goroutine forever (see #289).
func runCLIAgent(ctx context.Context, runner CommandRunner, workDir, prompt string,
	logger *slog.Logger, spec cliAgentSpec) (AgentResult, error) {
	// Guard against malformed specs: a nil parser makes the invocation
	// meaningless, and panicking would take down the whole daemon.
	if spec.parse == nil {
		return AgentResult{}, fmt.Errorf("cli agent spec for %q has no parser", spec.binary)
	}

	// Apply per-invocation timeout when configured.
	if spec.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.timeout)
		defer cancel()
	}

	// Start a watchdog goroutine that logs periodic warnings while the
	// agent is in flight. This makes hung invocations visible in the
	// journal instead of silently blocking.
	start := time.Now()
	watchdogDone := make(chan struct{})
	defer close(watchdogDone)
	if logger != nil {
		go func() {
			ticker := time.NewTicker(watchdogInterval)
			defer ticker.Stop()
			for {
				select {
				case <-watchdogDone:
					return
				case <-ticker.C:
					logger.Warn("agent still running",
						"binary", spec.binary,
						"elapsed", time.Since(start).Truncate(time.Second).String(),
						"workdir", workDir,
					)
				}
			}
		}()
	}

	var stdout, stderr []byte
	var err error

	if sr, ok := runner.(StreamingRunner); ok && logger != nil && spec.logLine != nil {
		stdout, stderr, err = sr.RunStreamWithStdin(ctx, workDir, prompt, func(line []byte) {
			spec.logLine(logger, line)
		}, spec.binary, spec.args...)
	} else {
		stdout, stderr, err = runner.RunWithStdin(ctx, workDir, prompt, spec.binary, spec.args...)
	}

	if err != nil {
		// Best-effort cost salvage: failed runs still bill the tokens they
		// consumed before dying.
		salvaged, _ := spec.parse(stdout)
		return AgentResult{CostUSD: salvaged.CostUSD}, fmt.Errorf("%s invocation failed: %w (stderr: %s)", spec.binary, err, string(stderr))
	}

	return spec.parse(stdout)
}
