package execx_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/qinqon/oompa/internal/execx"
)

func TestExecRunner_Run(t *testing.T) {
	r := &execx.ExecRunner{}
	stdout, stderr, err := r.Run(context.Background(), t.TempDir(), "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr %q)", err, stderr)
	}
	if got := string(stdout); got != "out\n" {
		t.Errorf("stdout = %q, want %q", got, "out\n")
	}
	// Contract: stderr is only captured on failure (from exec.ExitError);
	// successful runs return empty stderr.
	if len(stderr) != 0 {
		t.Errorf("stderr = %q, want empty on success", stderr)
	}
}

func TestExecRunner_RunReturnsErrorWithStderr(t *testing.T) {
	r := &execx.ExecRunner{}
	_, stderr, err := r.Run(context.Background(), t.TempDir(), "sh", "-c", "echo boom >&2; exit 3")
	if err == nil {
		t.Fatal("expected error for nonzero exit")
	}
	if !strings.Contains(string(stderr), "boom") {
		t.Errorf("stderr = %q, want it to contain %q", stderr, "boom")
	}
}

func TestExecRunner_RunWithStdin(t *testing.T) {
	r := &execx.ExecRunner{}
	stdout, _, err := r.RunWithStdin(context.Background(), t.TempDir(), "hello stdin", "cat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(stdout); got != "hello stdin" {
		t.Errorf("stdout = %q, want %q", got, "hello stdin")
	}
}

func TestExecRunner_EnvAndTokenVisibleToChild(t *testing.T) {
	r := &execx.ExecRunner{Env: []string{"OOMPA_TEST_VAR=abc"}}
	r.SetGHToken("tok123")
	stdout, _, err := r.Run(context.Background(), t.TempDir(), "sh", "-c", "printf '%s:%s' \"$OOMPA_TEST_VAR\" \"$GH_TOKEN\"")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(stdout); got != "abc:tok123" {
		t.Errorf("child env = %q, want %q", got, "abc:tok123")
	}
}

func TestExecRunner_RunStreamWithStdin(t *testing.T) {
	r := &execx.ExecRunner{}
	var lines []string
	stdout, _, err := r.RunStreamWithStdin(context.Background(), t.TempDir(), "",
		func(line []byte) { lines = append(lines, string(line)) },
		"sh", "-c", "echo one; echo two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 || lines[0] != "one" || lines[1] != "two" {
		t.Errorf("streamed lines = %v, want [one two]", lines)
	}
	if got := string(stdout); got != "one\ntwo\n" {
		t.Errorf("stdout = %q, want %q", got, "one\ntwo\n")
	}
}

func TestExecRunner_StreamHandlesLongLines(t *testing.T) {
	// A single line longer than the scanner's 64 KB initial buffer must
	// still be delivered whole (the scanner grows up to 10 MB).
	r := &execx.ExecRunner{}
	var lines []string
	stdout, stderr, err := r.RunStreamWithStdin(context.Background(), t.TempDir(), "",
		func(line []byte) { lines = append(lines, string(line)) },
		"sh", "-c", `head -c 100000 /dev/zero | tr '\0' 'x'; echo`)
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr %q)", err, stderr)
	}
	if len(lines) != 1 {
		t.Fatalf("expected exactly one streamed line, got %d", len(lines))
	}
	if len(lines[0]) != 100000 {
		t.Fatalf("expected a 100000-char line, got %d chars", len(lines[0]))
	}
	if len(stdout) != 100001 { // line + newline
		t.Errorf("stdout length = %d, want 100001", len(stdout))
	}
}

func TestExecRunner_StreamReturnsErrorOnFailure(t *testing.T) {
	r := &execx.ExecRunner{}
	var lines []string
	_, stderr, err := r.RunStreamWithStdin(context.Background(), t.TempDir(), "",
		func(line []byte) { lines = append(lines, string(line)) },
		"sh", "-c", "echo partial; echo broken >&2; exit 1")
	if err == nil {
		t.Fatal("expected error for nonzero exit")
	}
	if len(lines) != 1 || lines[0] != "partial" {
		t.Errorf("streamed lines before failure = %v, want [partial]", lines)
	}
	if !strings.Contains(string(stderr), "broken") {
		t.Errorf("stderr = %q, want it to contain %q", stderr, "broken")
	}
}

func TestExecRunner_CancelKillsProcessGroup(t *testing.T) {
	// Verify that cancelling the context kills the entire process group,
	// including grandchild processes spawned by the direct child.
	// The shell spawns a child ("sleep 300") via a subshell; without
	// process-group kill the sleep would survive.
	r := &execx.ExecRunner{}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// The parent shell spawns a grandchild sleep and writes its PID to
	// stdout so we can verify it was killed. The trap/wait ensures the
	// parent doesn't exit before the timeout fires.
	script := `
		sleep 300 &
		CHILD=$!
		echo $CHILD
		wait $CHILD
	`

	stdout, _, err := r.Run(ctx, t.TempDir(), "sh", "-c", script)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// Parse the grandchild PID and verify it is no longer running.
	pidStr := strings.TrimSpace(string(stdout))
	if pidStr == "" {
		t.Skip("could not capture grandchild PID (stdout empty)")
	}

	// Give the OS a moment to reap the killed processes.
	time.Sleep(100 * time.Millisecond)

	// Check if the grandchild is still running. "kill -0" checks existence
	// without sending a signal — if the process is gone, it returns an error.
	checkCmd := exec.Command("kill", "-0", pidStr)
	if checkErr := checkCmd.Run(); checkErr == nil {
		t.Errorf("grandchild process %s still running after context cancellation", pidStr)
		// Clean up the orphan to keep the test environment tidy.
		_ = exec.Command("kill", "-9", pidStr).Run()
	}
}

func TestExecRunner_StreamCancelKillsProcessGroup(t *testing.T) {
	// Same as above but through the streaming path.
	r := &execx.ExecRunner{}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	script := `
		sleep 300 &
		CHILD=$!
		echo $CHILD
		wait $CHILD
	`

	var lines []string
	stdout, _, err := r.RunStreamWithStdin(ctx, t.TempDir(), "",
		func(line []byte) { lines = append(lines, string(line)) },
		"sh", "-c", script)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// Try the streamed lines first, fall back to stdout buffer.
	pidStr := ""
	if len(lines) > 0 {
		pidStr = strings.TrimSpace(lines[0])
	} else {
		pidStr = strings.TrimSpace(string(stdout))
	}
	if pidStr == "" {
		t.Skip("could not capture grandchild PID")
	}

	time.Sleep(100 * time.Millisecond)

	checkCmd := exec.Command("kill", "-0", pidStr)
	if checkErr := checkCmd.Run(); checkErr == nil {
		t.Errorf("grandchild process %s still running after stream cancel", pidStr)
		_ = exec.Command("kill", "-9", pidStr).Run()
	}
}

func TestExecRunner_WaitDelayAllowsGracefulOutput(t *testing.T) {
	// Verify that context cancellation sends SIGTERM (not SIGKILL) and
	// the child gets a brief window (WaitDelay) to flush output. The
	// child traps SIGTERM, writes a post-signal marker, then exits.
	// Both pre-signal and post-signal output should be captured.
	r := &execx.ExecRunner{}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// The child writes BEFORE, blocks until SIGTERM, then writes AFTER
	// and exits. Both markers should appear in stdout.
	script := `
		trap 'echo AFTER; exit 0' TERM
		echo BEFORE
		sleep 300 &
		wait
	`
	stdout, _, err := r.Run(ctx, t.TempDir(), "sh", "-c", script)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(string(stdout), "BEFORE") {
		t.Errorf("expected pre-signal output containing BEFORE, got %q", stdout)
	}
	if !strings.Contains(string(stdout), "AFTER") {
		t.Errorf("expected post-signal output containing AFTER, got %q", stdout)
	}
}

// Cancellation must bound stdout reads and stop TERM-resistant descendants,
// even when the direct child exits gracefully before the grace period ends.
func TestExecRunner_CancelStubbornDescendant(t *testing.T) {
	for _, tt := range []struct {
		name        string
		streaming   bool
		detached    bool
		closeOutput bool
	}{
		{name: "buffered"},
		{name: "streaming", streaming: true},
		{name: "streaming-detached", streaming: true, detached: true},
		{name: "buffered-closed-output", closeOutput: true},
		{name: "streaming-closed-output", streaming: true, closeOutput: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			ready := filepath.Join(dir, "ready")
			r := &execx.ExecRunner{Env: []string{"EXECX_STUBBORN_READY=" + ready}}
			if tt.detached {
				r.Env = append(r.Env, "EXECX_STUBBORN_DETACH=1")
			}
			if tt.closeOutput {
				r.Env = append(r.Env, "EXECX_STUBBORN_CLOSE_OUTPUT=1")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type result struct {
				stdout []byte
				err    error
			}
			done := make(chan result, 1)
			go func() {
				args := []string{"-c", `trap 'echo AFTER; exit 0' TERM; "$1" -test.run=^TestExecRunner_StubbornHelper$ & wait`, "sh", executable}
				var stdout []byte
				var runErr error
				if tt.streaming {
					stdout, _, runErr = r.RunStreamWithStdin(ctx, dir, "", nil, "sh", args...)
				} else {
					stdout, _, runErr = r.Run(ctx, dir, "sh", args...)
				}
				done <- result{stdout: stdout, err: runErr}
			}()

			var pid int
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				data, readErr := os.ReadFile(ready)
				if readErr == nil {
					pid, err = strconv.Atoi(string(data))
					if err == nil {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("descendant did not become ready")
			}
			defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
			start := time.Now()
			cancel()
			select {
			case got := <-done:
				t.Logf("returned after %s: %v", time.Since(start), got.err)
				// Leave time for the liveness check below to fail before the
				// 5s timer could hide a missing early cleanup kill.
				if tt.closeOutput && time.Since(start) >= 2*time.Second {
					t.Errorf("closed-output command did not return promptly before escalation")
				}
				if got.err == nil {
					t.Error("expected cancellation error")
				}
				if !strings.Contains(string(got.stdout), "BEFORE\n") || !strings.Contains(string(got.stdout), "AFTER\n") {
					t.Errorf("lost partial or graceful output: %q", got.stdout)
				}
			case <-time.After(8 * time.Second):
				_ = syscall.Kill(pid, syscall.SIGKILL)
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("command did not return even after test cleanup")
				}
				t.Fatal("command still blocked 8s after cancellation (WaitDelay is 5s)")
			}
			if tt.detached {
				// A new session escapes group signals. Returning still must be
				// bounded by the stdout deadline; the test owns its cleanup.
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}

			// Orphans may remain zombies until init reaps them; kill(0) alone
			// cannot distinguish those from a surviving process.
			deadline = time.Now().Add(time.Second)
			for {
				state, checkErr := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
				if _, exited := checkErr.(*exec.ExitError); checkErr != nil && !exited {
					t.Fatalf("cannot check descendant state: %v", checkErr)
				}
				if checkErr != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("TERM-resistant descendant %d survived cancellation (state %q)", pid, state)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

func TestExecRunner_StubbornHelper(t *testing.T) {
	ready := os.Getenv("EXECX_STUBBORN_READY")
	if ready == "" {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	if os.Getenv("EXECX_STUBBORN_DETACH") == "1" {
		if _, err := syscall.Setsid(); err != nil {
			t.Fatal(err)
		}
	}
	// Keep only stdout open: an active stderr copier can mask the Scan hang
	// by causing os/exec's context watcher to close all pipes at WaitDelay.
	_ = os.Stderr.Close()
	fmt.Println("BEFORE")
	if os.Getenv("EXECX_STUBBORN_CLOSE_OUTPUT") == "1" {
		if err := os.Stdout.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(ready, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	// Self-terminate even if the test or runner fails to clean up.
	time.Sleep(30 * time.Second)
	os.Exit(0)
}
