# Systemd Deployment

This directory contains a native systemd unit for running oompa as a long-running service with automatic binary updates and restarts.

## Overview

The systemd unit replaces the `run-oompa.sh` wrapper script with a cleaner, more maintainable solution that:

- Downloads the latest `oompa` binary from GitHub releases before each start
- Runs oompa with `--exit-on-new-version` to trigger restarts when updates are available
- Automatically restarts on exit (new version or transient failures)
- Integrates with journald for centralized logging
- Supports multiple instances via systemd templates (e.g., `oompa@issue-resolver.service`, `oompa@pr-babysitter.service`)

## Prerequisites

- `gh` CLI installed and authenticated (`gh auth login`)
- Google Cloud Application Default Credentials configured
- systemd (Linux only)

## Quick Start

### 1. Install the service

```bash
cd deploy/systemd
./install.sh issue-resolver
```

This creates:
- `~/.config/systemd/user/oompa@.service` - The service template
- `~/.config/oompa/issue-resolver.env` - Configuration file for this instance

### 2. Configure the environment file

Edit `~/.config/oompa/issue-resolver.env`:

```bash
# GitHub authentication
GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Google Cloud Vertex AI
CLOUD_ML_REGION=us-east5
ANTHROPIC_VERTEX_PROJECT_ID=my-gcp-project

# Oompa command-line flags
OOMPA_FLAGS=--owner myorg --repo myrepo --label good-for-ai --poll-interval 2m
```

See `env.example` for all available options.

### 3. Start the service

```bash
systemctl --user start oompa@issue-resolver
systemctl --user status oompa@issue-resolver
```

### 4. Enable auto-start on boot

```bash
systemctl --user enable oompa@issue-resolver
loginctl enable-linger $USER  # Run even when not logged in
```

## Multiple Instances

You can run multiple oompa instances side-by-side for different use cases:

```bash
# Issue resolver
./install.sh issue-resolver
# Edit ~/.config/oompa/issue-resolver.env with: --owner myorg --repo myrepo --label good-for-ai

# PR babysitter
./install.sh pr-babysitter
# Edit ~/.config/oompa/pr-babysitter.env with: --owner myorg --repo myrepo --pr-numbers 123,456

# Start both
systemctl --user start oompa@issue-resolver oompa@pr-babysitter
```

## Managing the Service

### View logs

```bash
# Follow logs in real-time
journalctl --user -u oompa@issue-resolver -f

# View recent logs
journalctl --user -u oompa@issue-resolver -n 100

# View logs since last boot
journalctl --user -u oompa@issue-resolver -b
```

### Stop the service

```bash
systemctl --user stop oompa@issue-resolver
```

### Restart manually

```bash
systemctl --user restart oompa@issue-resolver
```

### Disable auto-start

```bash
systemctl --user disable oompa@issue-resolver
```

## How It Works

1. **ExecStartPre**: Downloads the latest `oompa-linux-amd64` binary from GitHub releases to `/tmp/oompa-bin/`
2. **ExecStart**: Runs the binary with `--exit-on-new-version=qinqon/oompa` plus user-configured flags
3. **On exit**: systemd waits 5 seconds (`RestartSec=5`) then loops back to step 1

This ensures oompa automatically picks up new releases without manual intervention.

## Troubleshooting

### Service fails to start

Check the logs:
```bash
journalctl --user -u oompa@issue-resolver -n 50
```

Common issues:
- Missing `GITHUB_TOKEN` or invalid GitHub App credentials
- Missing `CLOUD_ML_REGION` or `ANTHROPIC_VERTEX_PROJECT_ID`
- `gh` CLI not installed or not in PATH
- Invalid `OOMPA_FLAGS` syntax

### Binary download fails

Ensure:
- `gh` CLI is authenticated: `gh auth status`
- Network connectivity to `github.com`
- GitHub rate limits haven't been exceeded

### Service doesn't restart after new release

Check:
- Binary is running with `--exit-on-new-version=qinqon/oompa` flag
- Service restart policy is active: `systemctl --user show oompa@issue-resolver | grep Restart`

## System-wide Installation (Optional)

For system-wide deployment (runs as a dedicated user), install to `/etc/systemd/system/` instead:

```bash
sudo cp oompa@.service /etc/systemd/system/
sudo mkdir -p /etc/oompa
sudo cp env.example /etc/oompa/issue-resolver.env
# Edit /etc/oompa/issue-resolver.env
sudo systemctl daemon-reload
sudo systemctl start oompa@issue-resolver
sudo systemctl enable oompa@issue-resolver
```

Update the `User=` directive in the service file to specify the service account.

## Migration from run-oompa.sh

If you're currently using `run-oompa.sh`:

1. Note your current command-line arguments
2. Stop the wrapper script (Ctrl+C or kill the process)
3. Follow the installation steps above
4. Add your arguments to `OOMPA_FLAGS` in the env file
5. Start the systemd service

The systemd unit provides the same functionality with better process management and logging.
