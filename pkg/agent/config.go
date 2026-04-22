package agent

import "time"

// Config holds all agent configuration.
type Config struct {
	Owner         string
	Repo          string
	Label         string
	CloneDir      string
	PollInterval  time.Duration
	VertexRegion  string
	VertexProject string
	LogLevel      string
	LogFile       string
	DryRun        bool
	OneShot       bool
	SignedOffBy   string
	GitHubUser      string   // authenticated GitHub username (for reaction checks)
	GitHubToken     string   // GitHub token (passed to Claude for gh CLI access)
	GitHubHeadOwner string   // owner for PR head filtering (fork owner for PAT, repo owner for App)
	ForkOwner       string   // owner of the fork repo for pushing (empty = push to upstream)
	ForkRepo        string   // name of the fork repo (empty = same as Repo)
	GitAuthorName   string   // git commit author name
	GitAuthorEmail  string   // git commit author email
	Reviewers       []string // whitelist of users/bots whose reviews to address
	WatchPRs          []int    // PR numbers to monitor directly (bypasses issue discovery)
	Reactions         []string // which reactions to run: "reviews", "ci", "conflicts" (empty = all)
	CreateFlakyIssues bool     // when true, create issues for unrelated CI failures (opt-in)
	OnlyAssigned      bool     // when true, only process issues assigned to the agent user
	MaxWorkers        int      // maximum concurrent Claude invocations (1 = sequential, default)
	TriageJobs        []string // CI job URLs to monitor for periodic job triage

	// GitHub App authentication (alternative to GITHUB_TOKEN)
	GitHubAppID             int64
	GitHubAppPrivateKey     []byte
	GitHubAppInstallationID int64

	// Gemini code reviewer settings
	GeminiReviewer       bool     // enable Gemini PR reviews
	GeminiModel          string   // model to use (default: "gemini-2.0-flash-exp")
	GeminiReviewOn       []string // triggers: "new-pr", "push" (empty = ["new-pr", "push"])
	GeminiReviewSeverity string   // minimum severity to comment: "info", "warning", "error" (default: "warning")
}
