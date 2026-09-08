# Systemd

Oompa can run as a systemd user service that downloads the latest release binary on each (re)start.

## Credentials File

Store provider credentials in `~/.config/oompa/env`:

```bash
# For Vertex AI (used by both OpenCode and Claude Code)
CLOUD_ML_REGION=us-east5
ANTHROPIC_VERTEX_PROJECT_ID=my-gcp-project
GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
GOOGLE_CLOUD_PROJECT=my-gcp-project

# GITHUB_TOKEN is optional -- oompa falls back to `gh auth token`
```

## Optional Pi Service

The [Pi adapter](../configuration/pi.md) is optional; OpenCode remains the default. Use a binary containing it rather than assuming the latest release download includes it. During provisioning, install Pi `0.85.1`, Node.js `>=22.19.0`, Git, `gh`, Bash, `jq`, Python 3, and the [full CE checkout](../getting-started/installation.md#optional-pi) pinned to `caa3b231452a1cd444261d3c4d46bcd72d3246dd`. Do not install packages in `ExecStartPre` or during agent tasks.

Build from an Oompa source checkout at a verified revision containing `pkg/agent/pi.go`, using the [build prerequisites](../getting-started/installation.md#prerequisites), and install it during provisioning:

```bash
go build -o oompa ./cmd/oompa
sudo install -m 0755 oompa /usr/local/bin/oompa-pi
```

For the issue resolver unit below, create `~/.config/systemd/user/oompa-issue-resolver.service.d/pi.conf` (creating the parent directory if needed) before starting it:

```ini
[Service]
ExecStartPre=
ExecStart=
ExecStart=/usr/local/bin/oompa-pi --agent pi --repo myorg/myrepo --poll-interval 2m --log-level info
```

The empty directives clear the release download and original command. For the babysitter or triage unit, apply the same override to that unit's drop-in: clear `ExecStartPre` and `ExecStart`, then copy its original `ExecStart` arguments, replacing the executable with `/usr/local/bin/oompa-pi`, adding `--agent pi`, and removing `--exit-on-new-version=qinqon/oompa` if present. Leave `OOMPA_EXIT_ON_NEW_VERSION` and the YAML `exit-on-new-version` setting unset too: upgrades are manual for this provisioned binary, not release-driven restarts. After provisioning a replacement adapter-containing binary or changing the unit, run `systemctl --user daemon-reload` and restart the affected service.

For a dedicated Pi unit, add these values to its protected environment file, replacing the paths and model identifier:

```bash
OOMPA_AGENT=pi
OOMPA_AGENT_MODEL="<provider/model>"
OOMPA_CE_DIR=/home/oompa/compound-engineering-plugin
HOME=/home/oompa
PI_CODING_AGENT_DIR=/home/oompa/.pi/agent
XDG_CACHE_HOME=/home/oompa/.cache
# Alternatively use Pi /login during provisioning under this same identity.
ANTHROPIC_API_KEY=replace-with-provider-key
```

Keep explicit `--agent`/`--agent-model` arguments consistent with these values, since flags override environment variables. The unit's `PATH` must include both `pi` and the required Node binary plus the script tools; a shell's version-manager setup is not automatically loaded by systemd. Protect the credentials file (for example, mode `0600`) and use the same service user, `HOME`, and `PI_CODING_AGENT_DIR` for login and execution.

Ensure the dedicated home and cache are writable and private, and the CE checkout is readable. Sessions live under `os.UserCacheDir()/oompa/pi-sessions/<sha256>` per canonical worktree, not under the unit's `RuntimeDirectory`. Normal coding calls start fresh; resume uses `--continue`, falling back to fresh if no session exists. Summaries and issue matching use `--no-session` so they do not supersede persistent coding history. Cache lookup or directory-creation failures stop the invocation; they do not silently disable persistence.

Pi's trust flags are not a sandbox: model-issued Bash can run arbitrary commands as the service user. A user service shares that user's access, so do not use a personal account with unrelated credentials. Prefer a least-privilege isolated container with a dedicated user/home and no runtime package installs. See the [runtime flag semantics](../configuration/pi.md#skills-and-trust), and complete the [pending live acceptance checklist](../configuration/pi.md#pending-live-acceptance) before production use; local scripted-provider checks are not full live acceptance.

## Issue Resolver Service

`~/.config/systemd/user/oompa-issue-resolver.service`:

```ini
[Unit]
Description=Oompa Issue Resolver - myorg/myrepo
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=%h/.config/oompa/env
Environment=PATH=/usr/local/bin:/usr/bin:/bin
RuntimeDirectory=oompa-resolver
ExecStartPre=/bin/bash -c 'gh release download --repo qinqon/oompa --pattern oompa-linux-amd64 --dir %t/oompa-resolver --clobber && chmod +x %t/oompa-resolver/oompa-linux-amd64'
ExecStart=%t/oompa-resolver/oompa-linux-amd64 --exit-on-new-version=qinqon/oompa --repo myorg/myrepo --poll-interval 2m --log-level info
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
```

## PR Babysitter Service

`~/.config/systemd/user/oompa-pr-babysitter.service`:

```ini
[Unit]
Description=Oompa PR Babysitter - myorg/myrepo
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=%h/.config/oompa/env
Environment=PATH=/usr/local/bin:/usr/bin:/bin
RuntimeDirectory=oompa-babysitter
ExecStartPre=/bin/bash -c 'gh release download --repo qinqon/oompa --pattern oompa-linux-amd64 --dir %t/oompa-babysitter --clobber && chmod +x %t/oompa-babysitter/oompa-linux-amd64'
ExecStart=%t/oompa-babysitter/oompa-linux-amd64 --exit-on-new-version=qinqon/oompa --repo myorg/myrepo --watch-prs 123,456 --reactions ci,conflicts,rebase --fork myuser/myrepo --poll-interval 2m --log-level info
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
```

## Periodic CI Triage (Timer)

For one-shot workflows, use a `Type=oneshot` service paired with a systemd timer.

`~/.config/systemd/user/oompa-periodic-triage.service`:

```ini
[Unit]
Description=Oompa Periodic CI Triage - myorg/myrepo
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=%h/.config/oompa/env
Environment=PATH=/usr/local/bin:/usr/bin:/bin
RuntimeDirectory=oompa-periodic-triage
ExecStartPre=/bin/bash -c 'gh release download --repo qinqon/oompa --pattern oompa-linux-amd64 --dir %t/oompa-periodic-triage --clobber && chmod +x %t/oompa-periodic-triage/oompa-linux-amd64'
ExecStart=%t/oompa-periodic-triage/oompa-linux-amd64 --repo myorg/myrepo --triage-jobs https://prow.example.com/view/gs/bucket/logs/periodic-e2e-job/ --create-flaky-issues --one-shot --log-level info
```

`~/.config/systemd/user/oompa-periodic-triage.timer`:

```ini
[Unit]
Description=Run Oompa Periodic CI Triage daily at 9 AM

[Timer]
OnCalendar=*-*-* 09:00:00 Europe/Madrid
Persistent=true

[Install]
WantedBy=timers.target
```

Enable the timer (not the service):

```bash
systemctl --user enable --now oompa-periodic-triage.timer
```

## How It Works

For the release-downloading units above (without the Pi override):

- `ExecStartPre` downloads the latest release binary before each start
- `--exit-on-new-version` makes the agent exit when it detects a newer release during polling
- `Restart=always` restarts the service on exit, triggering `ExecStartPre` to download the new binary
- `RuntimeDirectory=` gives each unit its own directory under `/run/user/<uid>/`
- `Persistent=true` on timers ensures missed runs are caught up on next boot
- The timer's `OnCalendar` supports IANA timezones (e.g. `Europe/Madrid`, `US/Eastern`)

## Managing Services

```bash
# Enable and start
systemctl --user enable --now oompa-issue-resolver

# Check status
systemctl --user status oompa-issue-resolver

# View logs
journalctl --user -u oompa-issue-resolver -f

# Restart
systemctl --user restart oompa-issue-resolver
```
