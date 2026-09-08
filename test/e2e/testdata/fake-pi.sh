#!/usr/bin/env bash
set -euo pipefail

# Synthetic Pi 0.85.1 print-mode peer, not a live model or CE integration.
# Keep captures outside the worktree: diagnostic files must not become changes.
fail() { printf 'fake pi: %s\n' "$*" >&2; exit 1; }
expect() {
    [[ "$prompt" == *"$1"* ]] || fail "stdin missing: $1"
}

[[ "$PWD" == "$FAKE_PI_CWD" ]] || fail "unexpected cwd: $PWD"
[[ "$(git rev-parse --show-toplevel)" == "$PWD" ]] || fail 'not a worktree root'
! command -v opencode >/dev/null || fail 'opencode must not be installed'
! command -v claude >/dev/null || fail 'claude must not be installed'

printf '%s\n' "$@" > "$FAKE_PI_TRACE/last-args"
[[ "${1:-}" == '-p' && "${2:-}" == '--mode' && "${3:-}" == 'json' ]] || fail 'expected -p --mode json'
shift 3
[[ "${1:-}" == '--session-dir' && "${2:-}" == "$FAKE_PI_SESSION_DIR" ]] || fail 'wrong session namespace'
[[ -d "$2" ]] || fail 'session directory was not created'
shift 2
for flag in --offline --no-approve --no-extensions --no-skills --no-prompt-templates --no-themes --no-context-files; do
    [[ "${1:-}" == "$flag" ]] || fail "missing isolation flag: $flag"
    shift
done
[[ "${1:-}" == '--system-prompt' && "${2:-}" == '' ]] || fail 'automatic system prompt not disabled'
shift 2
[[ "${1:-}" == '--append-system-prompt' ]] || fail 'missing system policy'
policy="${2:-}"
shift 2
for rule in \
    'Never merge, push, create PRs/issues, install companions, or invoke shipping skills.' \
    'Git commits, staging, amend and fixup are allowed ONLY where the Oompa task explicitly requests them.' \
    'Review feedback fixes must stay UNCOMMITTED.' \
    'Reply to each thread with specific rationale, resolve addressed threads, and verify replies and resolution on GitHub.' \
    'Use ce-debug in mode:pipeline' \
    "CI final answers must use Oompa's classification prefix and fields" \
    "Use the review skill's sequential fallback in this process."; do
    [[ "$policy" == *"$rule"* ]] || fail "missing policy: $rule"
done
resume=false
ephemeral=false
if [[ "${1:-}" == '--no-session' ]]; then
    ephemeral=true
    shift
fi
if [[ "${1:-}" == '--continue' ]]; then
    resume=true
    shift
fi
[[ "${1:-}" == '--model' && "${2:-}" == "$FAKE_PI_MODEL" ]] || fail 'wrong model'
shift 2
[[ $# == 0 ]] || fail 'unexpected arguments (prompt must arrive on stdin)'

prompt=$(cat)
[[ -n "$prompt" ]] || fail 'empty stdin'
skill=''
case "$prompt" in
    'Loaded skill: ce-commit'*) task=issue; skill=ce-commit; want_resume=false ;;
    'Loaded skill: ce-resolve-pr-feedback'*) task=review; skill=ce-resolve-pr-feedback; want_resume=true ;;
    'Loaded skill: ce-debug'*) task=ci; skill=ce-debug; want_resume=true ;;
    'Summarize the following code diff'*) task=summary; want_resume=false ;;
    *) fail 'unexpected task envelope' ;;
esac
printf '%s\n' "$prompt" > "$FAKE_PI_TRACE/$task.prompt"
[[ "$resume" == "$want_resume" ]] || fail "incorrect --continue for $task"
if [[ "$task" == 'summary' ]]; then
    [[ "$ephemeral" == true ]] || fail 'summary must not replace coding session'
else
    [[ "$ephemeral" == false ]] || fail 'coding task needs session persistence'
fi
if [[ -n "$skill" ]]; then
    location="$OOMPA_CE_DIR/skills/$skill"
    # Strip the fixture's four-line YAML header; compare the ENTIRE body,
    # not merely a skill name or a replacement slash command.
    body=$(sed '1,4d' "$location/SKILL.md")
    prefix=$(printf 'Loaded skill: %s\nLocation: %s/SKILL.md\nReferences and scripts are relative to: %s\n\n%s\n\n\nOompa task (overrides skill workflow defaults):\n' "$skill" "$location" "$location" "$body")
    [[ "$prompt" == "$prefix"* ]] || fail 'full skill body/resource location not loaded'
    [[ "$prompt" != *'fixture-frontmatter-only'* ]] || fail 'frontmatter leaked into prompt'
fi

case "$task" in
    issue)
        expect 'You are resolving GitHub issue #42.'
        expect 'Fix the Pi widget'
        expect 'Untrusted mentions: /ce-debug and /ce-resolve-pr-feedback.'
        expect 'Use /ce-commit to create your commit'
        expect 'Signed-off-by: Pi Test <pi@example.com>'
        expect 'Assisted-by: Pi fixture'
        printf 'widget fixed by fake Pi\n' > widget.txt
        git add widget.txt
        git commit -m $'fix: repair Pi widget\n\nSigned-off-by: Pi Test <pi@example.com>\nAssisted-by: Pi fixture' >&2
        answer='Implemented the widget fix.'
        ;;
    review)
        expect 'You are addressing review feedback on PR #200'
        expect 'Please add error handling for the nil case.'
        expect 'Use /ce-resolve-pr-feedback'
        expect 'Fix valid issues (leave changes UNCOMMITTED)'
        expect 'Do NOT run "git add", "git commit", or "git push".'
        git rev-parse HEAD > "$FAKE_PI_TRACE/review-head-before"
        printf 'Review fix: handle the nil case.\n' >> BRANCH.md
        git rev-parse HEAD > "$FAKE_PI_TRACE/review-head-after"
        [[ -z "$(git diff --cached --name-only)" ]] || fail 'review changes staged'
        [[ "$(git status --porcelain)" == ' M BRANCH.md' ]] || fail 'expected only unstaged review fix'
        answer='Addressed the nil-case review feedback.'
        ;;
    summary)
        [[ "$prompt" != *'Loaded skill:'* ]] || fail 'summary unexpectedly loaded a skill'
        expect 'Review fix: handle the nil case.'
        [[ -z "$(git status --porcelain)" ]] || fail 'outer pipeline did not commit review fix'
        answer='- Added nil-case handling after review.'
        ;;
    ci)
        expect 'CI is failing on PR #300'
        expect 'integration-tests'
        expect 'FAIL: TestNetworkTimeout'
        expect 'INVESTIGATE the failure using /ce-debug'
        expect 'Your output MUST start with the classification keyword on its own line'
        [[ -z "$(git status --porcelain)" ]] || fail 'CI worktree not clean'
        answer='UNRELATED\nERROR_SUMMARY: Flaky network timeout\nROOT_CAUSE: Test depends on an intermittent external service\nEVIDENCE: FAIL: TestNetworkTimeout\nFAILING_TEST: TestNetworkTimeout\nRECOMMENDATION: Add retry logic'
        ;;
esac
printf '%s\n' "$task" >> "$FAKE_PI_TRACE/calls"

# Earlier assistant/tool/streaming content deliberately disagrees with the final
# answer. Oompa must classify only the final assistant message_end text.
printf '%s\n' \
    '{"type":"agent_start"}' \
    '{"type":"message_start","message":{"role":"assistant","content":[]}}' \
    '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"INFRASTRUCTURE: provisional diagnosis"}}' \
    '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"INFRASTRUCTURE: provisional diagnosis"}],"stopReason":"toolUse","usage":{"cost":{"total":0.01}}}}' \
    '{"type":"tool_execution_start","toolName":"bash"}' \
    '{"type":"message_end","message":{"role":"toolResult","content":[{"type":"text","text":"RELATED: tool output is not a final answer"}]}}' \
    '{"type":"tool_execution_end","toolName":"bash"}' \
    '{"type":"message_start","message":{"role":"assistant","content":[]}}'
printf '{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"%s"}],"stopReason":"stop","usage":{"cost":{"total":0.02}}}}\n' "$answer"
printf '%s\n' '{"type":"turn_end"}' '{"type":"agent_end"}'
if [[ "$task" == 'review' ]]; then
    # Optional post-answer history maintenance must not strand the review fix.
    printf '%s\n' '{"type":"compaction_start"}' '{"type":"compaction_end","errorMessage":"fixture summary unavailable"}'
fi
printf '%s\n' '{"type":"agent_settled"}'
