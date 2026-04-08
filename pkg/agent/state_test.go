package agent

import (
	"context"
	"log/slog"
	"testing"
)

func TestNewState(t *testing.T) {
	s := NewState()
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if len(s.ActiveIssues) != 0 {
		t.Errorf("expected empty ActiveIssues, got %d", len(s.ActiveIssues))
	}
}

func TestBuildStateFromGitHub_NoIssues(t *testing.T) {
	gh := &mockGitHubClient{}
	cfg := Config{Owner: "owner", Repo: "repo", Label: "good-for-ai"}

	state := BuildStateFromGitHub(context.Background(), gh, cfg, "/tmp/clone", slog.Default())

	if len(state.ActiveIssues) != 0 {
		t.Errorf("expected empty state, got %d issues", len(state.ActiveIssues))
	}
}

func TestBuildStateFromGitHub_RecoversPRState(t *testing.T) {
	gh := &mockGitHubClient{
		issues: []Issue{{Number: 42, Title: "Fix bug", Labels: []string{"good-for-ai"}}},
		prs:    []PR{{Number: 100, State: "open", Head: "ai/issue-42"}},
		prComments: []ReviewComment{
			{ID: 10, User: "alice", Body: "looks good"},
			{ID: 20, User: "bob", Body: "fix this"},
		},
	}
	cfg := Config{Owner: "owner", Repo: "repo", Label: "good-for-ai"}

	state := BuildStateFromGitHub(context.Background(), gh, cfg, "/tmp/clone", slog.Default())

	work, ok := state.ActiveIssues[42]
	if !ok {
		t.Fatal("expected issue 42 in state")
	}
	if work.PRNumber != 100 {
		t.Errorf("expected PR 100, got %d", work.PRNumber)
	}
	if work.Status != "pr-open" {
		t.Errorf("expected status 'pr-open', got %q", work.Status)
	}
	if work.LastCommentID != 0 {
		t.Errorf("expected lastCommentID 0 (reactions used instead), got %d", work.LastCommentID)
	}
}

func TestBuildStateFromGitHub_RecoversFailedState(t *testing.T) {
	gh := &mockGitHubClient{
		issues: []Issue{{Number: 99, Title: "Hard bug", Labels: []string{"good-for-ai", "ai-failed"}}},
	}
	cfg := Config{Owner: "owner", Repo: "repo", Label: "good-for-ai"}

	state := BuildStateFromGitHub(context.Background(), gh, cfg, "/tmp/clone", slog.Default())

	work, ok := state.ActiveIssues[99]
	if !ok {
		t.Fatal("expected issue 99 in state")
	}
	if work.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", work.Status)
	}
}

func TestBuildStateFromGitHub_SkipsNewIssues(t *testing.T) {
	gh := &mockGitHubClient{
		issues: []Issue{{Number: 1, Title: "New issue", Labels: []string{"good-for-ai"}}},
	}
	cfg := Config{Owner: "owner", Repo: "repo", Label: "good-for-ai"}

	state := BuildStateFromGitHub(context.Background(), gh, cfg, "/tmp/clone", slog.Default())

	if len(state.ActiveIssues) != 0 {
		t.Error("new issues without PRs should not be in recovered state")
	}
}
