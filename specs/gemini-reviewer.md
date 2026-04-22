# Gemini Reviewer

Defines a `GeminiClient` interface for PR code review using Google Gemini via Vertex AI.

## Interface

```go
type GeminiClient interface {
    ReviewPRDiff(ctx context.Context, diff, prompt string) ([]ReviewSuggestion, error)
}
```

## Types

```go
type ReviewSuggestion struct {
    FilePath string
    Line     int
    Severity string // "info", "warning", "error"
    Message  string
}
```

## Concrete Implementation

`VertexGeminiClient` uses the Google Cloud Vertex AI SDK to call Gemini models.

```go
type VertexGeminiClient struct {
    projectID string
    region    string
    model     string // e.g. "gemini-2.5-pro"
}

func NewVertexGeminiClient(projectID, region, model string) (*VertexGeminiClient, error)
```

## Configuration

Added to `Config` struct:

```go
type Config struct {
    // ... existing fields ...
    GeminiReviewer       bool     // enable Gemini PR reviews
    GeminiModel          string   // model to use (default: "gemini-2.0-flash-exp")
    GeminiReviewOn       []string // triggers: "new-pr", "push" (empty = ["new-pr", "push"])
    GeminiReviewSeverity string   // minimum severity to comment: "info", "warning", "error" (default: "warning")
}
```

## Environment Variables and Flags

| Env Var | Flag | Default | Description |
|---------|------|---------|-------------|
| `OOMPA_GEMINI_REVIEWER` | `--gemini-reviewer` | `false` | Enable Gemini PR reviews |
| `OOMPA_GEMINI_MODEL` | `--gemini-model` | `gemini-2.0-flash-exp` | Gemini model to use |
| `OOMPA_GEMINI_REVIEW_ON` | `--gemini-review-on` | `new-pr,push` | When to trigger reviews (comma-separated: new-pr, push) |
| `OOMPA_GEMINI_REVIEW_SEVERITY` | `--gemini-review-severity` | `warning` | Minimum severity to comment on (info, warning, error) |

Gemini uses the same Vertex AI credentials as Claude (`--vertex-project` and `--vertex-region`).

## Review Prompt

```
You are reviewing a pull request. Analyze the diff and identify:
- Bugs or logic errors
- Missing error handling
- Potential nil pointer dereferences
- Security concerns (SQL injection, XSS, command injection)
- Race conditions or concurrency issues

For each issue, provide:
1. File path
2. Line number (in the new version)
3. Severity: "error" (must fix), "warning" (should fix), or "info" (suggestion)
4. Clear description of the issue and suggested fix

Focus on correctness and safety. Ignore style unless it affects correctness.

Output format: JSON array of objects with fields: file_path, line, severity, message.
```

## Posting Reviews

When Gemini returns suggestions:
1. Filter by configured severity threshold
2. Group by file path
3. Post as GitHub PR review comments using `client.PullRequests.CreateComment()`
4. Add bot marker to each comment: `<!-- oompa-gemini -->`
5. Prefix each comment with severity emoji:
   - 🔴 error
   - ⚠️ warning
   - 💡 info

## Integration with Main Loop

New method on `Agent`:

```go
func (a *Agent) ProcessGeminiReviews(ctx context.Context)
```

Called after `ProcessReviewComments` in the main loop. For each PR in state:
1. Check if PR has new commits since last Gemini review
2. Check if trigger matches config (new-pr or push)
3. Get PR diff via `git diff origin/main...HEAD`
4. Call `gemini.ReviewPRDiff(ctx, diff, prompt)`
5. Filter by severity threshold
6. Post comments via `AddPRComment` (inline, line-specific)
7. Track last reviewed commit in state

## State Tracking

Add to `IssueWork`:

```go
type IssueWork struct {
    // ... existing fields ...
    LastGeminiReviewSHA string `json:"lastGeminiReviewSHA"` // last commit SHA reviewed by Gemini
}
```

## Deduplication

- Skip review if `LastGeminiReviewSHA == current head SHA`
- Only review when PR head advances
- Do not re-review on force pushes that rewrite history to the same tree

## Tests (`gemini_test.go`)

Mock `GeminiClient`:

- `TestReviewPRDiff_Success` -- mock returns suggestions, verify parsing
- `TestReviewPRDiff_EmptyDiff` -- returns no suggestions for empty diff
- `TestReviewPRDiff_FiltersBySeverity` -- only surfaces issues >= threshold
- `TestReviewPRDiff_InvalidJSON` -- handles malformed response

Integration tests in `loop_test.go`:

- `TestProcessGeminiReviews_HappyPath` -- posts comments, updates state
- `TestProcessGeminiReviews_SkipsAlreadyReviewed` -- no action when SHA matches
- `TestProcessGeminiReviews_FiltersSeverity` -- respects severity threshold
- `TestProcessGeminiReviews_Disabled` -- no action when GeminiReviewer=false
- `TestProcessGeminiReviews_NewPROnly` -- only reviews on new PR when configured
- `TestProcessGeminiReviews_PushOnly` -- only reviews on push when configured
