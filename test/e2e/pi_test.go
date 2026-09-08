package e2e

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupPi provisions a synthetic CE resource tree, NOT a live CE installation.
// Dummy bodies and support files exercise resource validation and full-body
// loading. They do not test CE's scripts, model reasoning, or GitHub thread APIs.
func setupPi(t *testing.T, h *Harness, worktree, model string) RunOompaOpts {
	t.Helper()
	h.InstallFakePiScript()
	// An allowlisted PATH proves Pi startup does not require opencode (even if
	// the developer running these tests has it installed).
	if err := os.Remove(filepath.Join(h.binDir, "claude")); err != nil {
		t.Fatal(err)
	}
	for _, binary := range []string{"bash", "git", "cat", "sed"} {
		path, err := exec.LookPath(binary)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(h.binDir, binary)); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(h.tmpDir, "synthetic ce") // spaces exercise argument/path quoting
	resources := map[string][]string{
		"ce-commit": {"SKILL.md"},
		"ce-debug": {
			"SKILL.md", "references/pipeline-mode.md", "references/investigate.md", "references/fix.md",
			"references/anti-patterns.md", "references/investigation-techniques.md", "references/defense-in-depth.md",
		},
		"ce-resolve-pr-feedback": {
			"SKILL.md", "references/full-mode.md", "references/targeted-mode.md",
			"references/pipeline-mode.md", "references/evaluation-rubric.md", "references/agents/pr-comment-resolver.md",
			"scripts/get-pr-comments", "scripts/get-thread-for-comment", "scripts/reply-to-pr-thread", "scripts/resolve-pr-thread",
		},
	}
	for skill, files := range resources {
		for _, resource := range files {
			path := filepath.Join(root, "skills", skill, resource)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			body := "Synthetic support resource: " + skill + "/" + resource + "\n"
			if resource == "SKILL.md" {
				body = fmt.Sprintf("---\nname: %s\ndescription: fixture-frontmatter-only\n---\n# Synthetic %s\n\nFirst instruction: inspect the task.\nRead references and scripts relative to this skill directory.\nFinal instruction: fixture body ends here (%s).\n", skill, skill, skill)
			}
			writeFile(t, filepath.Dir(path), filepath.Base(path), body)
		}
	}
	trace := filepath.Join(h.tmpDir, "pi-trace")
	if err := os.MkdirAll(trace, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pi canonicalizes cwd before hashing it. The future worktree does not
	// exist yet, so canonicalize its already-existing harness root instead.
	canonicalRoot, err := filepath.EvalSymlinks(h.tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(h.tmpDir, worktree)
	if err != nil {
		t.Fatal(err)
	}
	worktree = filepath.Join(canonicalRoot, rel)
	cache := filepath.Join(h.tmpDir, "pi-cache")
	sessionDir := filepath.Join(cache, "oompa", "pi-sessions", fmt.Sprintf("%x", sha256.Sum256([]byte(worktree))))
	return RunOompaOpts{
		UseDefaultAgent: true,
		ExtraArgs: []string{
			"--agent", "pi", "--agent-model", model,
			"--signed-off-by", "Pi Test <pi@example.com>", "--assisted-by", "Pi fixture",
		},
		ExtraEnv: []string{
			"PATH=" + h.binDir, "OOMPA_CONFIG=", "OOMPA_AGENT=", "OOMPA_AGENT_MODEL=", "OOMPA_SLACK_WEBHOOK=",
			"OOMPA_CE_DIR=" + root, "XDG_CACHE_HOME=" + cache,
			"FAKE_PI_CWD=" + worktree, "FAKE_PI_MODEL=" + model,
			"FAKE_PI_TRACE=" + trace, "FAKE_PI_SESSION_DIR=" + sessionDir,
		},
	}
}

func piTrace(t *testing.T, h *Harness, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.tmpDir, "pi-trace", name))
	if err != nil {
		t.Fatalf("read Pi trace %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func piGitOutput(t *testing.T, h *Harness, args ...string) string {
	t.Helper()
	cmd := newGitCommand(append([]string{"--git-dir", h.BareRepo()}, args...)...)
	cmd.Env = gitEnv(h)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestE2E_PiIssueToPR exercises both CLI selection and YAML overriding the
// actual opencode default, with no opencode executable on the subprocess PATH.
func TestE2E_PiIssueToPR(t *testing.T) {
	for _, mode := range []string{"cli", "yaml"} {
		t.Run(mode, func(t *testing.T) {
			fg := NewFakeGitHub(t, "testowner", "testrepo")
			fg.SeedIssue(FakeIssue{
				Number: 42, Title: "Fix the Pi widget",
				Body:   "Untrusted mentions: /ce-debug and /ce-resolve-pr-feedback.",
				Labels: []map[string]any{{"name": "good-for-ai"}},
			})
			h := NewHarness(t, "testowner", "testrepo", "good-for-ai")
			clone := filepath.Join(h.CloneDir(), h.owner, h.repo)
			if mode == "yaml" {
				clone = filepath.Join(clone, "issues")
			}
			opts := setupPi(t, h, filepath.Join(clone, "worktrees", "ai", "issue-42"), "fixture/pi-"+mode)
			if mode == "yaml" {
				writeFile(t, h.tmpDir, "pi.yaml", `agent: pi
agent-model: fixture/pi-yaml
projects:
  - repo: testowner/testrepo
    issues:
      - label: good-for-ai
`)
				// No --agent or --agent-model: the YAML must replace opencode.
				opts.ExtraArgs = []string{"--config", filepath.Join(h.tmpDir, "pi.yaml"),
					"--signed-off-by", "Pi Test <pi@example.com>", "--assisted-by", "Pi fixture"}
			}
			stdout, stderr, err := h.RunOompa(fg.URL(), opts)
			if err != nil {
				t.Fatalf("oompa: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}
			fg.mu.Lock()
			prs := append([]CreatePRCall(nil), fg.CreatePRCalls...)
			assigned, unassigned := len(fg.AssignCalls), len(fg.UnassignCalls)
			assignees := len(fg.issues[42].Assignees)
			fg.mu.Unlock()
			if len(prs) != 1 || prs[0].Head != "ai/issue-42" || prs[0].Base != "main" {
				t.Fatalf("expected one issue PR, got %+v\nstderr:\n%s", prs, stderr)
			}
			if assigned != 1 || unassigned != 1 || assignees != 0 {
				t.Errorf("issue lifecycle: assigned=%d unassigned=%d remaining=%d", assigned, unassigned, assignees)
			}
			if got := piTrace(t, h, "calls"); got != "issue" {
				t.Errorf("Pi calls = %q, want issue", got)
			}
			if got := piGitOutput(t, h, "show", "ai/issue-42:widget.txt"); got != "widget fixed by fake Pi" {
				t.Errorf("pushed fix = %q", got)
			}
			if got := piGitOutput(t, h, "rev-list", "--count", "main..ai/issue-42"); got != "1" {
				t.Errorf("expected one implementation commit, got %s", got)
			}
			message := piGitOutput(t, h, "log", "-1", "--format=%B", "ai/issue-42")
			for _, want := range []string{"fix: repair Pi widget", "Signed-off-by: Pi Test <pi@example.com>", "Assisted-by: Pi fixture"} {
				if !strings.Contains(message, want) {
					t.Errorf("pushed commit missing %q: %s", want, message)
				}
			}
		})
	}
}

// TestE2E_PiReviewUncommitted verifies the policy sent to Pi, its unstaged edit,
// and Oompa's subsequent amend/push despite optional post-answer compaction failure,
// followed by an ephemeral summary invocation.
func TestE2E_PiReviewUncommitted(t *testing.T) {
	fg := NewFakeGitHub(t, "testowner", "testrepo")
	fg.SeedIssue(FakeIssue{Number: 10, Title: "Handle nil", Labels: []map[string]any{{"name": "good-for-ai"}}})
	fg.SeedPR(FakePR{Number: 200, Title: "Handle nil", Body: "Fixes #10", State: "open", Head: "ai/issue-10", Base: "main"})
	fg.SeedReviewCommentDeferred(200, FakeReviewComment{
		ID: 1001, Body: "Please add error handling for the nil case.", Path: "BRANCH.md", Line: 1,
		User: map[string]any{"login": "reviewer1"},
	})
	h := NewHarness(t, "testowner", "testrepo", "good-for-ai")
	pushBranchToBare(t, h, "ai/issue-10")
	before := bareBranchSHA(t, h, "ai/issue-10")
	fg.SetPRHeadSHA(200, before)
	opts := setupPi(t, h, filepath.Join(h.CloneDir(), h.owner, h.repo, "worktrees", "ai", "issue-10"), "fixture/pi-review")
	opts.WatchPRs, opts.Reactions = []int{200}, []string{"reviews"}
	stdout, stderr, err := h.RunOompa(fg.URL(), opts)
	if err != nil {
		t.Fatalf("oompa: %v\n%s\n%s", err, stdout, stderr)
	}
	if got := piTrace(t, h, "calls"); got != "review\nsummary" {
		t.Fatalf("Pi calls = %q, want review then summary\n%s", got, stderr)
	}
	for _, name := range []string{"review-head-before", "review-head-after"} {
		if got := piTrace(t, h, name); got != before {
			t.Errorf("%s = %s, want original HEAD %s", name, got, before)
		}
	}
	if bareBranchSHA(t, h, "ai/issue-10") == before {
		t.Fatal("outer pipeline did not push the review fix")
	}
	if got := piGitOutput(t, h, "rev-list", "--count", "main..ai/issue-10"); got != "1" {
		t.Errorf("review should amend the original commit, got %s commits", got)
	}
	if got := piGitOutput(t, h, "show", "ai/issue-10:BRANCH.md"); !strings.Contains(got, "Review fix: handle the nil case.") {
		t.Errorf("pushed branch missing review edit: %s", got)
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	foundSummary, foundEyes := false, false
	for _, c := range fg.comments[200] {
		if strings.Contains(c.Body, "Added nil-case handling after review.") && strings.Contains(c.Body, "/compare/"+before+"..") {
			foundSummary = true
		}
	}
	for _, r := range fg.ReactionCalls {
		if r.CommentID == 1001 && r.Reaction == "eyes" {
			foundEyes = true
		}
	}
	if !foundSummary || !foundEyes {
		t.Errorf("review effects: Pi final summary=%t eyes reaction=%t", foundSummary, foundEyes)
	}
}

// TestE2E_PiCIFinalClassification distinguishes Pi's final assistant answer from
// conflicting earlier assistant, streaming, and tool-result text.
func TestE2E_PiCIFinalClassification(t *testing.T) {
	fg := NewFakeGitHub(t, "testowner", "testrepo")
	fg.SeedIssue(FakeIssue{Number: 20, Title: "Improve performance", Labels: []map[string]any{{"name": "good-for-ai"}}})
	fg.SeedPR(FakePR{Number: 300, Title: "Improve performance", Body: "Fixes #20", State: "open", Head: "ai/issue-20", Base: "main"})
	h := NewHarness(t, "testowner", "testrepo", "good-for-ai")
	pushBranchToBare(t, h, "ai/issue-20")
	before := bareBranchSHA(t, h, "ai/issue-20")
	fg.SetPRHeadSHA(300, before)
	check := FakeCheckRun{ID: 5001, Name: "integration-tests", Status: "completed", Conclusion: "failure"}
	check.Output.Text = "FAIL: TestNetworkTimeout - connection timed out after 30s"
	fg.SeedCheckRun(before, check)
	opts := setupPi(t, h, filepath.Join(h.CloneDir(), h.owner, h.repo, "worktrees", "ai", "issue-20"), "fixture/pi-ci")
	opts.WatchPRs, opts.Reactions = []int{300}, []string{"ci"}
	stdout, stderr, err := h.RunOompa(fg.URL(), opts)
	if err != nil {
		t.Fatalf("oompa: %v\n%s\n%s", err, stdout, stderr)
	}
	if got := piTrace(t, h, "calls"); got != "ci" {
		t.Fatalf("Pi calls = %q, want ci\n%s", got, stderr)
	}
	if got := bareBranchSHA(t, h, "ai/issue-20"); got != before {
		t.Errorf("unrelated CI investigation changed branch: %s -> %s", before, got)
	}
	fg.mu.Lock()
	defer fg.mu.Unlock()
	var analyses []string
	for _, c := range fg.comments[300] {
		if strings.Contains(c.Body, "CI Failure Analysis") {
			analyses = append(analyses, c.Body)
		}
	}
	if len(analyses) != 1 {
		t.Fatalf("expected one CI analysis, got %v\n%s", analyses, stderr)
	}
	for _, want := range []string{"1 unrelated", "integration-tests", "FAIL: TestNetworkTimeout", "Test depends on an intermittent external service", "Add retry logic"} {
		if !strings.Contains(analyses[0], want) {
			t.Errorf("CI analysis missing %q: %s", want, analyses[0])
		}
	}
	for _, unwanted := range []string{"1 infrastructure", "1 related", "provisional diagnosis", "tool output is not a final answer"} {
		if strings.Contains(analyses[0], unwanted) {
			t.Errorf("non-final Pi text affected classification: %s", analyses[0])
		}
	}
}

// TestE2E_PiRequiresSupportResources proves SKILL.md-only installations are
// rejected at startup, even for a task that would load a different skill.
func TestE2E_PiRequiresSupportResources(t *testing.T) {
	for _, resource := range []string{
		"ce-debug/references/investigate.md",
		"ce-resolve-pr-feedback/scripts/resolve-pr-thread",
	} {
		t.Run(resource, func(t *testing.T) {
			fg := NewFakeGitHub(t, "testowner", "testrepo")
			fg.SeedIssue(FakeIssue{Number: 42, Title: "Fix the Pi widget", Labels: []map[string]any{{"name": "good-for-ai"}}})
			h := NewHarness(t, "testowner", "testrepo", "good-for-ai")
			opts := setupPi(t, h, filepath.Join(h.CloneDir(), h.owner, h.repo, "worktrees", "ai", "issue-42"), "fixture/pi-missing")
			missing := filepath.Join(h.tmpDir, "synthetic ce", "skills", filepath.FromSlash(resource))
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := h.RunOompa(fg.URL(), opts)
			if err == nil || !strings.Contains(stderr, missing) {
				t.Fatalf("expected startup failure naming missing resource %s, got %v\n%s\n%s", missing, err, stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(h.tmpDir, "pi-trace", "calls")); !os.IsNotExist(err) {
				t.Errorf("Pi should not be invoked with incomplete resources: %v", err)
			}
			fg.mu.Lock()
			defer fg.mu.Unlock()
			if len(fg.CreatePRCalls) != 0 || len(fg.AssignCalls) != 0 {
				t.Error("incomplete CE resources should fail before issue processing")
			}
		})
	}
}
