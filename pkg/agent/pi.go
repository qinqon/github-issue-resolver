package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// PiAgent implements the Pi 0.85.1 print-mode JSON protocol.
type PiAgent struct {
	Model   string
	Timeout time.Duration
}

type piUsage struct {
	Cost struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

type piMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage        *piUsage `json:"usage"`
	StopReason   string   `json:"stopReason"`
	ErrorMessage string   `json:"errorMessage"`
}

type piEvent struct {
	Type    string    `json:"type"`
	Message piMessage `json:"message"`
	Usage   *piUsage  `json:"usage"`
	Result  struct {
		Usage *piUsage `json:"usage"`
	} `json:"result"`
	AssistantMessageEvent struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent"`
	ToolName     string `json:"toolName"`
	ErrorMessage string `json:"errorMessage"`
	Aborted      bool   `json:"aborted"`
}

// parsePiResult counts authoritative message_end events, not the copies in
// turn_end/agent_end. Streaming usage replaces the current response estimate;
// it is retained if the process dies before that response's message_end.
func parsePiResult(stdout []byte) (AgentResult, error) {
	var result AgentResult
	var last piMessage
	var pendingCost float64
	var settled bool
	var streamErr error
	var compactionErr error
	var compactionAborted bool
	for line := range bytes.SplitSeq(stdout, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event piEvent
		if err := json.Unmarshal(line, &event); err != nil {
			streamErr = fmt.Errorf("invalid or truncated Pi JSON event: %w", err)
			continue // Still salvage cost from subsequent valid events.
		}
		switch event.Type {
		case "agent_start":
			// A failed preflight compaction can be followed by a successful prompt.
			compactionErr = nil
			compactionAborted = false
			settled = false
		case "turn_start", "auto_retry_start", "compaction_start":
			settled = false
		case "message_start":
			if event.Message.Role == "assistant" {
				result.CostUSD += pendingCost
				pendingCost = 0
				last = piMessage{}
				settled = false
			}
		case "message_update":
			settled = false
			if event.Usage != nil {
				pendingCost = max(0, event.Usage.Cost.Total)
			}
		case "message_end":
			if event.Message.Role == "assistant" {
				last = event.Message
				settled = false
				if last.Usage != nil {
					pendingCost = max(0, last.Usage.Cost.Total)
				}
				result.CostUSD += pendingCost
				pendingCost = 0
			}
		case "compaction_end":
			if event.Result.Usage != nil {
				result.CostUSD += max(0, event.Result.Usage.Cost.Total)
			}
			if event.Aborted || event.ErrorMessage != "" {
				compactionAborted = event.Aborted
				compactionErr = fmt.Errorf("pi compaction failed (aborted=%t): %s", event.Aborted, event.ErrorMessage)
			}
		case "agent_settled":
			settled = true
		}
	}
	result.CostUSD += pendingCost
	if streamErr != nil {
		return result, streamErr
	}
	// Failed optional history maintenance does not undo a settled answer.
	if compactionErr != nil && (compactionAborted || !settled || last.StopReason != "stop") {
		return result, compactionErr
	}
	if last.StopReason != "stop" {
		return result, fmt.Errorf("pi did not complete: stopReason=%q %s", last.StopReason, last.ErrorMessage)
	}
	if !settled {
		return result, fmt.Errorf("pi output missing final agent_settled event")
	}
	var text strings.Builder
	for _, content := range last.Content {
		if content.Type == "text" {
			text.WriteString(content.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return result, fmt.Errorf("pi completed without a final assistant answer")
	}
	result.Result = text.String()
	return result, nil
}

func logPiEvent(logger *slog.Logger, line []byte) {
	var event piEvent
	if json.Unmarshal(line, &event) != nil {
		return
	}
	switch event.Type {
	case "tool_execution_start", "tool_execution_end":
		logger.Debug("pi tool", "event", event.Type, "tool", event.ToolName)
	case "message_update":
		if event.AssistantMessageEvent.Type == "text_delta" {
			logger.Debug("pi", "text", previewText(event.AssistantMessageEvent.Delta))
		}
	case "message_end":
		if event.Message.Role == "assistant" {
			logger.Info("pi response finished", "reason", event.Message.StopReason, "usage", event.Message.Usage, "error", event.Message.ErrorMessage)
		}
	case "compaction_end":
		if event.Aborted || event.ErrorMessage != "" {
			logger.Warn("pi compaction failed", "aborted", event.Aborted, "error", event.ErrorMessage)
		} else {
			logger.Info("pi lifecycle", "event", event.Type)
		}
	case "auto_retry_start", "compaction_start", "agent_settled":
		logger.Info("pi lifecycle", "event", event.Type, "error", event.ErrorMessage)
	}
}

// Run uses an Oompa-only session namespace. Pi --continue filters session headers
// by cwd and starts fresh if none exists; it never resumes another backend.
func (p *PiAgent) Run(ctx context.Context, runner CommandRunner, workDir, prompt string,
	logger *slog.Logger, resume bool) (AgentResult, error) {
	workDir, err := filepath.Abs(workDir)
	if err != nil {
		return AgentResult{}, err
	}
	workDir, err = filepath.EvalSymlinks(workDir)
	if err != nil {
		return AgentResult{}, fmt.Errorf("pi worktree: %w", err)
	}
	// Pi 0.85.1 migrates commands before applying resource/trust flags.
	// Refuse rather than let startup mutate even a read-only task's worktree.
	commands := filepath.Join(workDir, ".pi", "commands")
	if info, err := os.Lstat(commands); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return AgentResult{}, fmt.Errorf("pi refuses symlinked commands path %s; remove the symlink and provision the repository's .pi/prompts layout before using this backend", commands)
		}
		if _, err := os.Stat(filepath.Join(workDir, ".pi", "prompts")); err != nil {
			return AgentResult{}, fmt.Errorf("pi would migrate %s at startup; provision the repository's .pi/prompts layout before using this backend", commands)
		}
	} else if !os.IsNotExist(err) {
		return AgentResult{}, fmt.Errorf("pi project migration preflight: %w", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return AgentResult{}, fmt.Errorf("pi session cache: %w", err)
	}
	// The operator's cache may be shared for reading or reached via a symlink;
	// never chmod it. Resolve it before checking the Oompa-owned namespace.
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return AgentResult{}, fmt.Errorf("pi session cache: %w", err)
	}
	cacheDir, err = filepath.EvalSymlinks(cacheDir)
	if err != nil {
		return AgentResult{}, fmt.Errorf("pi session cache: %w", err)
	}
	sessionDir := filepath.Join(cacheDir, "oompa", "pi-sessions", fmt.Sprintf("%x", sha256.Sum256([]byte(workDir))))
	// Validate each parent before creating/using children: MkdirAll alone accepts
	// existing permissive directories and symlinks. Fail closed without altering
	// existing session data or permissions.
	for _, dir := range []string{cacheDir, filepath.Join(cacheDir, "oompa"), filepath.Dir(sessionDir), sessionDir} {
		forbidden := os.FileMode(0o077)
		if dir == cacheDir {
			forbidden = 0o022 // Other users must not be able to replace oompa.
		} else if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
			return AgentResult{}, fmt.Errorf("pi session directory %s: %w", dir, err)
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return AgentResult{}, fmt.Errorf("pi session directory %s: %w", dir, err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || uint64(stat.Uid) != uint64(os.Geteuid()) || info.Mode().Perm()&forbidden != 0 {
			return AgentResult{}, fmt.Errorf("pi session directory %s must be a real directory owned by the current user with no %03o permission bits", dir, forbidden)
		}
	}
	// Auxiliary answers must not replace the coding history selected by --continue.
	ephemeral := strings.HasPrefix(prompt, changeSummaryPromptPrefix) ||
		strings.HasPrefix(prompt, flakyMatchPromptPrefix) || strings.HasPrefix(prompt, triageMatchPromptPrefix)
	prompt, err = preparePiPrompt(prompt)
	if err != nil {
		return AgentResult{}, err
	}
	args := []string{"-p", "--mode", "json", "--session-dir", sessionDir,
		"--offline", "--no-approve", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-context-files",
		"--system-prompt", "",
		"--append-system-prompt", piTaskPolicy}
	if ephemeral {
		args = append(args, "--no-session")
	} else if resume {
		args = append(args, "--continue")
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	return runCLIAgent(ctx, runner, workDir, prompt, logger, cliAgentSpec{
		binary: "pi", args: args, logLine: logPiEvent, parse: parsePiResult, timeout: p.Timeout,
	})
}
