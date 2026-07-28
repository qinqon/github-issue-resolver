package execx_test

import (
	"context"
	"os/exec"
	"strings"
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
