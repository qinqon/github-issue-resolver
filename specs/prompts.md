# Prompt Templates

## `buildImplementationPrompt(issue Issue, signedOffBy string) string`

Tells Claude to:
- Read Claude.md for conventions
- Implement the fix, run `make lint` and `make test`
- Commit (no trailing period, 72 char body)
- If signedOffBy is non-empty, add `Signed-off-by:` to every commit message
- Do NOT push, create PRs, or amend — the agent handles that automatically

## `buildReviewResponsePrompt(work IssueWork, comments []ReviewComment, reviews []PRReview, owner, repo, signedOffBy string) string`

Tells Claude to:
- Address all review feedback (both review requests and inline comments)
- For each inline comment, reply using `gh api repos/OWNER/REPO/pulls/comments/COMMENT_ID/replies`
- Run `make lint` and `make test`
- If signedOffBy is non-empty, add `Signed-off-by:` to every commit message
- Do NOT commit, push, or amend — the agent handles that automatically

## `buildCIFixPrompt(work IssueWork, failures []CheckRun, diff string, commits []Commit, signedOffBy string) string`

Tells Claude to:
- Investigate whether CI failures are DIRECTLY caused by the PR changes
- If UNRELATED: output must start with "UNRELATED" followed by explanation; do not fix
- If RELATED: output must start with "RELATED"; fix the code and run `make lint` and `make test`
- For multi-commit PRs: create a fixup commit targeting the commit that introduced the issue
- For single-commit PRs: stage changes but do not commit (agent will amend)
- If signedOffBy is non-empty (and multi-commit PR), add `Signed-off-by:` to every commit message
- Do NOT push or rebase — the agent handles that automatically

## `buildConflictResolutionPrompt(work IssueWork, originDefaultBranch, signedOffBy string) string`

Tells Claude to:
- Run `git fetch origin` to get the latest changes
- Run `git rebase <originDefaultBranch>` to rebase on top of the latest main branch
- Resolve any merge conflicts while keeping the PR's functionality intact
- Run `make lint` and `make test` to verify the resolved code still works
- If signedOffBy is non-empty, add `Signed-off-by:` to every commit message
- Do NOT push — the agent handles that automatically

## Tests (`prompt_test.go`)

- `TestBuildImplementationPrompt` -- verifies issue number, title, body are interpolated; verifies push/PR instructions are absent
- `TestBuildReviewResponsePrompt` -- verifies each comment's file/line/body is included
