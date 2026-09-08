// Package execx provides external command execution with optional
// line-streaming, used to run git and coding-agent CLIs.
package execx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// scannerInitBufSize is the initial buffer size for the bufio.Scanner
	// used when streaming command output (64 KB).
	scannerInitBufSize = 64 * 1024
	// scannerMaxBufSize is the maximum buffer size the scanner will grow to
	// when reading long lines from command output (10 MB).
	scannerMaxBufSize = 10 * 1024 * 1024
)

// CommandRunner executes external commands.
type CommandRunner interface {
	Run(ctx context.Context, workDir string, name string, args ...string) (stdout []byte, stderr []byte, err error)
	RunWithStdin(ctx context.Context, workDir string, stdin string, name string, args ...string) (stdout []byte, stderr []byte, err error)
}

// StreamingRunner extends CommandRunner with line-by-line stdout streaming.
type StreamingRunner interface {
	CommandRunner
	RunStreamWithStdin(ctx context.Context, workDir string, stdin string, onLine func(line []byte), name string, args ...string) (stdout []byte, stderr []byte, err error)
}

// ExecRunner is the concrete CommandRunner using os/exec.
type ExecRunner struct {
	// Env holds additional environment variables to set on commands.
	Env []string
	mu  sync.RWMutex // protects Env
}

// SetGHToken updates the GH_TOKEN environment variable in a thread-safe manner.
func (r *ExecRunner) SetGHToken(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Update existing GH_TOKEN or append if not found
	const ghTokenKey = "GH_TOKEN="
	for i, env := range r.Env {
		if strings.HasPrefix(env, ghTokenKey) {
			r.Env[i] = ghTokenKey + token
			return
		}
	}
	r.Env = append(r.Env, ghTokenKey+token)
}

// command builds an exec.Cmd with the runner's env overlay applied.
// Runner-level variables are appended after the process environment;
// os/exec documents last-wins semantics for duplicate keys, so overlay
// values (e.g. refreshed GH_TOKEN) shadow inherited ones.
//
// The child is started in its own process group (Setpgid) so that context
// cancellation signals descendants that remain in that group. cleanup must be
// called after Run/Wait to stop escalation and kill any remaining group members.
func (r *ExecRunner) command(ctx context.Context, workDir, stdin, name string, args ...string) (cmd *exec.Cmd, cleanup func()) {
	cmd = exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	// Start the child in its own process group so we can kill the
	// entire tree on context cancellation. Without this, only the
	// direct child receives the signal and grandchildren may orphan.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Send SIGTERM to the entire process group when the context is
	// cancelled, giving children a chance to flush output and clean up.
	// Go's WaitDelay kills only the direct child, so escalate the group
	// ourselves even if that child exits before its descendants.
	var escalation *time.Timer
	escalated := make(chan struct{})
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Send SIGTERM to the whole process group for graceful shutdown.
		// SIGTERM (vs SIGKILL) allows the child to flush partial results
		// (e.g. cost data) before exiting.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		// ESRCH means the process already exited between the context
		// cancellation check and the kill call — treat as success without
		// arming escalation that could signal a reused process group ID.
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		escalation = time.AfterFunc(cmd.WaitDelay, func() {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			close(escalated)
		})
		return err
	}

	// Give the child a brief window to handle SIGTERM and flush output
	// before the I/O pipes are forcibly torn down.
	cmd.WaitDelay = 5 * time.Second

	r.mu.RLock()
	if len(r.Env) > 0 {
		cmd.Env = append(cmd.Environ(), r.Env...)
	}
	r.mu.RUnlock()
	return cmd, func() {
		// Run/Wait joins the context watcher, so Cancel has finished writing
		// escalation. Never leave a timer that might later signal a reused PID.
		if escalation != nil {
			if escalation.Stop() {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			} else {
				<-escalated
			}
		}
	}
}

func (r *ExecRunner) Run(ctx context.Context, workDir, name string, args ...string) (stdout, stderr []byte, err error) {
	return r.RunWithStdin(ctx, workDir, "", name, args...)
}

func (r *ExecRunner) RunWithStdin(ctx context.Context, workDir, stdin, name string, args ...string) (stdout, stderr []byte, err error) {
	cmd, cleanup := r.command(ctx, workDir, stdin, name, args...)
	defer cleanup()
	stdout, err = cmd.Output()
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr = exitErr.Stderr
	}
	return stdout, stderr, err
}

func (r *ExecRunner) RunStreamWithStdin(ctx context.Context, workDir, stdin string, onLine func(line []byte), name string, args ...string) (stdout, stderr []byte, err error) {
	cmd, cleanup := r.command(ctx, workDir, stdin, name, args...)
	defer cleanup()

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	// StdoutPipe returns an os.Pipe reader. Bound Scan independently of Wait:
	// descendants can retain stdout after closing stderr, leaving no active
	// os/exec copier to force the pipes closed when WaitDelay expires.
	cancel := cmd.Cancel
	cmd.Cancel = func() error {
		cancelErr := cancel()
		deadlineErr := pipe.(*os.File).SetReadDeadline(time.Now().Add(cmd.WaitDelay))
		return errors.Join(cancelErr, deadlineErr)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	var stdoutBuf bytes.Buffer
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, scannerInitBufSize), scannerMaxBufSize)
	for scanner.Scan() {
		line := scanner.Bytes()
		stdoutBuf.Write(line)
		stdoutBuf.WriteByte('\n')
		if onLine != nil {
			onLine(append([]byte{}, line...))
		}
	}
	scanErr := scanner.Err()

	// Close the pipe before Wait: if the scanner stopped early (e.g. a line
	// exceeded the buffer cap), unread output could block the child forever,
	// and Wait must not be called until reads complete or the pipe is closed.
	// After a normal EOF this is a harmless no-op.
	pipe.Close() //nolint:errcheck,gosec // best-effort unblock before Wait

	// Always call Wait to release child process resources and avoid zombies.
	waitErr := cmd.Wait()

	// Join both errors when present: the scanner error describes the read
	// failure, the wait error preserves the process exit status.
	if scanErr != nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), errors.Join(scanErr, waitErr)
	}
	return stdoutBuf.Bytes(), stderrBuf.Bytes(), waitErr
}
