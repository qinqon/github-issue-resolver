# Prompt Templates

## `buildImplementationPrompt(issue Issue, signedOffBy string) string`

Tells Claude to:
- Read Claude.md for conventions
- Implement the fix, run `make lint` and `make test`
- Commit (no trailing period, 72 char body)
- If signedOffBy is non-empty, add `Signed-off-by:` to every commit message
- Do NOT push, create PRs, or amend — the agent handles that automatically

## `buildReviewResponsePrompt(work IssueWork, comments []ReviewComment, signedOffBy string) string`

Tells Claude to:
- For each review comment: implement if valid, push back with explanation if not
- Always reply to every comment, even when implementing the suggestion
- Reply using `gh pr review` or `gh api`
- Run lint/test, commit, push
- No force-push
- If signedOffBy is non-empty, add `Signed-off-by:` to every commit message

## Tests (`prompt_test.go`)

- `TestBuildImplementationPrompt` -- verifies issue number, title, body are interpolated; verifies push/PR instructions are absent
- `TestBuildReviewResponsePrompt` -- verifies each comment's file/line/body is included
