# Installation

## Prerequisites

- Go 1.26+
- A coding agent CLI on `PATH`: [OpenCode](https://opencode.ai) (recommended), [Claude Code](https://docs.anthropic.com/en/docs/claude-code), or optional Pi
- Provider credentials configured (e.g. `gcloud auth application-default login` for Vertex AI, or `ANTHROPIC_API_KEY` for direct API)
- GitHub authentication: either `gh auth login` (recommended), a personal access token (PAT) with repo scope, or a GitHub App
- `gh` CLI installed and configured as a git credential helper (`gh auth setup-git`)
- [compound-engineering-plugin](https://github.com/EveryInc/compound-engineering-plugin) for CI investigation, review handling, and commit creation

## Install the Coding Agent Plugin

**OpenCode:**

```bash
bunx @every-env/compound-plugin install compound-engineering --to opencode
```

**Claude Code:**

```
/plugin install compound-engineering
```

## Optional Pi

Pi is an implemented optional backend; OpenCode remains the default. Use an Oompa build containing the adapter and provision these pinned dependencies:

| Dependency | Pin / Requirement |
|------------|-------------------|
| Pi npm package | `@earendil-works/pi-coding-agent@0.85.1` |
| Node.js | `>=22.19.0` |
| Compound Engineering (CE) | `caa3b231452a1cd444261d3c4d46bcd72d3246dd` |
| Script tools on `PATH` | Git, `gh`, Bash, `jq`, Python 3 |

After provisioning Node.js and the script tools, install Pi and clone the full CE repository:

```bash
npm install -g @earendil-works/pi-coding-agent@0.85.1
git clone https://github.com/EveryInc/compound-engineering-plugin.git "$HOME/compound-engineering-plugin"
git -C "$HOME/compound-engineering-plugin" checkout --detach caa3b231452a1cd444261d3c4d46bcd72d3246dd
export OOMPA_CE_DIR="$HOME/compound-engineering-plugin"
```

Set `OOMPA_CE_DIR` to the **absolute checkout root**, not a plugin or skills subdirectory. Keep the full checkout, including referenced scripts and resources, available to the service. Oompa reads complete required skill bodies and embeds them in the prompt with location/script references intact, rather than merely replacing slash-command spelling.

CE also offers the official Pi package installation:

```bash
pi install git:github.com/EveryInc/compound-engineering-plugin@caa3b231452a1cd444261d3c4d46bcd72d3246dd
```

That is not required for Oompa and does not replace its explicit `OOMPA_CE_DIR` checkout. Oompa needs no Pi extension or companion packages. Neither `pi-subagents` nor `pi-ask-user` is required or loaded; CE review uses the sequential fallback.

Provision dependencies at image-build or service-setup time, with **no runtime package installs**. Local runtime checks used the actual adapter and Pi with a scripted localhost provider, not a real model. See [authentication](authentication.md#pi-authentication) and [Pi configuration](../configuration/pi.md) for service identity, trust boundaries, validation scope, and the pending full live acceptance checklist.

## Binary Download

Download the latest release binary from GitHub:

```bash
gh release download --repo qinqon/oompa --pattern 'oompa-linux-amd64'
chmod +x oompa-linux-amd64
sudo mv oompa-linux-amd64 /usr/local/bin/oompa
```

## Build From Source

```bash
git clone https://github.com/qinqon/oompa.git
cd oompa
go build -o oompa ./cmd/oompa
```

## Container Image

A container image is published to GitHub Container Registry on every push to `main`:

```bash
podman pull ghcr.io/qinqon/oompa:latest
```

The existing image includes Go, `gh` CLI, Claude Code CLI, and git. It is unchanged by the optional Pi adapter and does **not** include Pi or the pinned CE checkout. Pi requires a custom derivation with Node.js `>=22.19.0` and the dependencies above; do not assume a `node:22` base alone proves the minimum patch version. See [Kubernetes deployment](../operations/kubernetes.md#optional-pi-image).
