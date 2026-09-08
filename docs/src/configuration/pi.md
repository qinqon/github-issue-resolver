# Pi Backend

Pi is an implemented optional coding-agent backend. These instructions describe its integration contract for an Oompa build containing the adapter; they do not assert that a released binary already includes it. OpenCode (`opencode`) remains the default backend.

## Pins and Validation Status

The pinned versions are Pi `0.85.1` from npm package `@earendil-works/pi-coding-agent`, requiring Node.js `>=22.19.0`, and Compound Engineering (CE) commit `caa3b231452a1cd444261d3c4d46bcd72d3246dd` (`caa3b231`).

Validation has three distinct scopes:

- Automated fixtures cover command construction, prompt contents, session selection, JSON event parsing, and accounting without a real provider.
- Seventy local runtime checks passed using the actual Oompa adapter and installed Pi `0.85.1` bundled launcher (`dist/bundle/cli.js`) against a scripted localhost provider on September 8, 2026. They covered final answers, tool execution, provider errors, reported costs, fresh/continued/isolated sessions, auxiliary history preservation, full pinned CE debug-body transport, automatic-resource suppression, missing-package installation prevention, and the legacy-project migration guard. These were not merely fake-CLI fixtures; they did not exercise real-model decisions or actual compaction.
- Full live acceptance with a real model, execution of referenced CE scripts, and real GitHub issue/review/CI workflows remains pending. Scripted responses do not demonstrate that a model followed policy or that GitHub mutations occurred. The checklist below tracks that remaining work.

## Setup and Selection

Follow [installation](../getting-started/installation.md#optional-pi) for the pinned npm package and full CE checkout, and [authentication](../getting-started/authentication.md#pi-authentication) for provider credentials. Git, `gh`, Bash, `jq`, and Python 3 must be on `PATH` for the referenced scripts. Provision all dependencies before starting the agent, not through runtime package installs.

```bash
export OOMPA_CE_DIR="/absolute/path/to/compound-engineering-plugin"
oompa --agent pi --agent-model '<provider/model>' --repo myorg/myrepo
```

Replace `<provider/model>` with an identifier available in the configured Pi provider. Selection uses the existing `agent`/`agent-model` YAML keys or `OOMPA_AGENT`/`OOMPA_AGENT_MODEL` environment variables; there are no new Pi-specific selection flags. `OOMPA_CE_DIR` is an environment variable pointing to the absolute repository checkout root, not a skills subdirectory or YAML option.

Pi authentication can use interactive `/login` during setup or a provider variable such as `ANTHROPIC_API_KEY`. Login and Oompa must share the same service identity, `HOME`, and, if set, `PI_CODING_AGENT_DIR`. Unattended runs must not depend on a login prompt.

## Invocation and Sessions

The adapter invokes Pi in print/JSON mode with a session directory and explicit runtime trust flags:

```text
pi -p --mode json --session-dir <worktree-session-directory>
  --offline --no-approve --no-extensions --no-skills --no-prompt-templates
  --no-themes --no-context-files
  --system-prompt '' --append-system-prompt <Oompa-policy>
```

This is a schematic invocation, not a shell command to run manually. Oompa supplies the task prompt and model selection. Normal coding invocations start a fresh persistent session. Resume invocations add `--continue`; when no session exists, they start fresh instead. Auxiliary diff summaries and CI issue-matching calls use `--no-session` instead of continuation, so they do not supersede the persistent coding history selected by a later resume.

Sessions are isolated per canonical worktree path under:

```text
os.UserCacheDir()/oompa/pi-sessions/<sha256>
```

`<sha256>` is the hash of the canonical worktree path. On Linux, `os.UserCacheDir()` normally uses `$XDG_CACHE_HOME`, or `$HOME/.cache` when unset. Aliases of the same canonical path share the session directory; different canonical worktrees do not. Keep the cache writable by the service user and private because it can contain prompts, code, and tool output. Losing the cache loses continuation history, not Oompa's GitHub-rebuilt work state. `PI_CODING_AGENT_DIR` configures Pi's own configuration/authentication, not this session-directory mapping.

Failure to resolve the worktree or cache path, or to create the session directory, stops the invocation before Pi starts; Oompa does not silently switch to another session location. This setup is required even for auxiliary `--no-session` calls. A missing session is the fresh-start case, not a fallback for cache errors.

Existing Oompa-managed cache directories (`oompa`, `pi-sessions`, and the worktree directory) must be owned by the runtime user, have no group/other permissions, and not be symlinks. The resolved global cache must be owned by that user and not group/other-writable. Unsafe directories fail preflight without changing permissions or existing history; provision private directories under the service identity rather than relying on Oompa to repair shared storage.

Pi 0.85.1 automatically migrates `.pi/commands` to `.pi/prompts` before applying resource flags. Oompa rejects worktrees requiring that migration, including symlinked command directories, rather than let startup modify repository files during a read-only task. Migrate that layout separately before selecting Pi. Provision Pi's dedicated global profile before service startup as well, since Pi can migrate its own profile files.

## Skills and Trust

Oompa reads the **full bodies** of required CE skills from `OOMPA_CE_DIR` and embeds them in the prompt, preserving skill locations and script/resource references. Merely rewriting slash-command spelling does not load a skill. The full pinned checkout must remain accessible so referenced resources can be read and scripts can run.

The official CE Pi package install is available as `pi install git:github.com/EveryInc/compound-engineering-plugin@caa3b231452a1cd444261d3c4d46bcd72d3246dd`, but it does not replace Oompa's explicit checkout requirement. No extension or companion packages are needed or loaded. In particular, `pi-subagents` and `pi-ask-user` are not required; CE review uses its sequential fallback rather than subagents or interactive questions.

All automatic resource loading is disabled: extensions, skills, prompt templates, themes, and context files. Oompa also explicitly controls both system-prompt discovery paths:

| Pi Flag | Runtime Effect |
|---------|----------------|
| `--offline` | Prevents startup package installs and startup network access; does not block model/provider calls or tool-issued network access |
| `--no-approve` | Rejects project trust without prompting; does not grant permissions |
| `--system-prompt ''` | Disables discovered `SYSTEM.md` while retaining Pi's built-in system prompt |
| `--append-system-prompt <Oompa-policy>` | Supplies Oompa's policy explicitly, overriding append-prompt discovery |

The policy instructs Pi to read repository `AGENTS.md` or `CLAUDE.md` as conventions, not as authorization to load or trust repository code. Repository instructions and embedded skills remain subordinate to Oompa's Pi-specific system policy.

**These flags are not a sandbox.** The model's Bash tool can execute arbitrary commands with the runtime user's permissions. Disabling automatic loading and rejecting project trust do not make a malicious repository safe. Use a least-privilege isolated container, a dedicated user and home, narrowly scoped credentials, restricted mounts/network access, and no runtime package installs. Do not expose a personal home, host container socket, or unrelated secrets. See [deployment requirements](../operations/kubernetes.md#optional-pi-image).

## Unattended Policy

The Pi-specific system policy instructs the model to override conflicting repository or CE workflow instructions. It forbids interactive questions and never permits Pi to merge, push, or create PRs/issues. Oompa owns shipping and orchestration; humans own merges. These are model instructions, not OS-enforced Git restrictions or verified real-model behavior.

Commit permission is task-specific, not a blanket prohibition or blanket authorization:

| Task | Pi Policy |
|------|-----------|
| Issue implementation | Create the requested issue commit, including the supplied `Signed-off-by` and `Assisted-by` trailers; do not push or create a PR |
| Review response | Address feedback and perform the requested real GitHub replies/thread resolutions; leave code changes uncommitted for Oompa |
| CI repair | Amend or create a fixup only when the task explicitly authorizes it, preserving required trailers; no changes or commits for read-only investigation |
| Other tasks | Follow only the task's explicit commit permission and trailer requirements; do not infer permission from a CE shipping workflow |

CE debug runs use `mode:pipeline`: Oompa's task policy overrides generic shipping steps and the skill's normal structured-output format. CI output must serve Oompa's classification parser, starting with `RELATED`, `UNRELATED`, or `INFRASTRUCTURE`, followed by the requested structured fields such as `ERROR_SUMMARY`, `ROOT_CAUSE`, `EVIDENCE`, `RECOMMENDATION`, and `FAILING_TEST` when applicable. It must not substitute a generic debug report or proceed to shipping. Read-only tasks stay read-only even when a related fix is identified.

For periodic CI triage, preserve that task's requested `## Classification` section and `FLAKY_TEST`, `INFRASTRUCTURE`, or `CODE_BUG` values instead of the PR CI format.

## Costs and Budgets

Pi costs are **reported estimates**, derived from assistant streaming/`message_end` usage and reported `compaction_end` usage, not independent billing measurements. The adapter retains usage already reported on failed or interrupted calls, including the latest assistant streaming estimate when no final message arrives. It cannot recover usage that Pi or the provider never reports: failures, timeouts, truncated output, and interrupted compaction can undercount charges. In particular, compaction interrupted before its usage event may incur an unreported cost. Missing provider pricing counts as zero, so a zero reported cost does not mean the provider charged nothing.

A failed optional compaction is logged as a warning and does not discard a subsequently settled successful answer. Aborted compaction, incomplete output, and assistant errors still fail the invocation.

Oompa checks the estimated per-PR session budget before primary review/CI processing and each CI investigation. Auxiliary summaries and issue-matching calls currently do not recheck that budget and may still run after it is exhausted. This shared caller-level limitation affects every backend and requires a separate orchestration fix. In-flight calls and unreported usage can also exceed the configured threshold; this is not a hard billing cap. Use provider-side billing controls and least-privilege credentials for financial limits.

## Pending Live Acceptance

Run these checks against the pinned versions with a real configured model/provider and a disposable repository before production use. Record provider/model, exact versions and Pi entry point, Oompa revision, and observed results separately from automated fixtures and scripted localhost runtime checks. None of these full live acceptance checks is claimed as completed here.

- [ ] Authenticate as the actual service user with the deployed `HOME`/`PI_CODING_AGENT_DIR`; confirm Pi, Node, CE resources, and script dependencies are available without runtime installs.
- [ ] Exercise the deployed Pi launcher and referenced CE scripts with real model-driven tasks; verify script execution and results rather than prompt inclusion alone.
- [ ] Resolve a real issue, verify the intended commit and required `Signed-off-by`/`Assisted-by` trailers, and confirm only Oompa performs push/PR creation.
- [ ] Address a real review, verify actual GitHub replies and appropriate thread resolution rather than claims in model text, and confirm Pi leaves code changes uncommitted for Oompa.
- [ ] Investigate representative `RELATED`, `UNRELATED`, and `INFRASTRUCTURE` CI failures; verify Oompa accepts the classification and structured fields. Confirm a read-only task changes nothing, and a repair uses only the explicitly authorized amend/fixup behavior.
- [ ] Verify fresh normal coding sessions, continuation within the same canonical worktree, fresh fallback when a resume session is missing, and isolation between different worktrees. Confirm summaries and issue matching do not supersede coding history, and cache failures are surfaced.
- [ ] Confirm no questions, approval waits, companion loading, unauthorized commits, pushes, PR creation, or merges by Pi, including when repository/CE instructions request them.
- [ ] Compare reported assistant and compaction cost estimates with provider usage, including failures and interrupted compaction; exercise missing-pricing behavior and verify primary-call budget stops, accounting for the auxiliary-call gap above rather than treating the threshold as a billing cap.
