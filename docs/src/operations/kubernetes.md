# Kubernetes

Oompa can be deployed as a Kubernetes Deployment using the container image published to GitHub Container Registry.

## Container Image

```
ghcr.io/qinqon/oompa:latest
```

The image includes Go, `gh` CLI, Claude Code CLI, and git. It runs as a non-root user (UID 1000) and is compatible with OpenShift's random UID assignment.

The existing image remains unchanged; Pi is optional and is **not included**, nor is its required pinned CE checkout. The deployment below remains a non-Pi example.

## Optional Pi Image

For the optional Pi backend, build a custom derivation of the Oompa image with a binary containing the adapter; do not assume a published release already has it. Provision the following at **image-build time**, not in an init container, startup hook, or agent task:

- Node.js `>=22.19.0`. Check the actual version in the derived image; a `node:22` base tag alone is not a minimum-patch guarantee.
- Pi via `npm install -g @earendil-works/pi-coding-agent@0.85.1`.
- Git, `gh`, Bash, `jq`, and Python 3 for CE scripts, plus the tools needed by the target repository.
- A full CE checkout at commit `caa3b231452a1cd444261d3c4d46bcd72d3246dd`, for example at `/opt/compound-engineering-plugin`. Use the [clone and checkout commands](../getting-started/installation.md#optional-pi), with that absolute destination. Keep scripts and referenced resources intact and runtime-readable; prefer a read-only checkout.

Select your custom image in the Deployment and set `--agent=pi` and optionally `--agent-model=<provider/model>`, or use `OOMPA_AGENT`/`OOMPA_AGENT_MODEL`. Set `OOMPA_CE_DIR=/opt/compound-engineering-plugin`. No Pi extension, `pi-subagents`, or `pi-ask-user` is required or loaded; the official CE Pi package installation does not replace the explicit checkout.

Inject provider credentials through a Secret, for example `ANTHROPIC_API_KEY`, or provision Pi login credentials under the same runtime `HOME`/`PI_CODING_AGENT_DIR`. Do not bake credentials into the image. Mount a dedicated writable home/configuration directory and private cache accessible to the actual runtime UID, including random-UID deployments. On Linux, an explicit `XDG_CACHE_HOME=/cache` places worktree-scoped sessions under `/cache/oompa/pi-sessions/<sha256>`. Persist that cache only if continuation across pod restarts is desired; losing it causes resume to start fresh. The full [session contract](../configuration/pi.md#invocation-and-sessions) uses canonical worktree paths, so relocating worktrees changes the session mapping.

Run as a least-privilege non-root user in an isolated container with a dedicated home, dropped capabilities, no privilege escalation, narrowly scoped GitHub/provider credentials, restricted mounts and network access, and a read-only root filesystem where practical. Provide writable work/cache mounts explicitly. Do not mount personal homes or host container sockets, and do not permit runtime package installs. Pi's `--offline` prevents startup package installs/network access, not provider calls; `--no-approve` rejects project trust and does not grant permissions. These flags and the auto-loading restrictions are **not a sandbox**: model-issued Bash can execute arbitrary commands within the container's permissions.

Local runtime checks with a scripted localhost provider do not establish production acceptance. Complete the [pending live acceptance checklist](../configuration/pi.md#pending-live-acceptance) with a real model, CE scripts, and GitHub workflows before production use. Configure provider-side billing limits; Oompa's estimated costs and between-call budget check are not a billing cap.

## Example Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: oompa
  namespace: oompa
  labels:
    app: oompa
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: oompa
  template:
    metadata:
      labels:
        app: oompa
    spec:
      containers:
        - name: oompa
          image: ghcr.io/qinqon/oompa:latest
          args:
            - --repo=myorg/myrepo
            - --clone-dir=/work
            - --log-level=debug
            - --poll-interval=2m
          env:
            - name: GITHUB_APP_ID
              valueFrom:
                secretKeyRef:
                  name: oompa-github
                  key: app-id
            - name: GITHUB_APP_INSTALLATION_ID
              valueFrom:
                secretKeyRef:
                  name: oompa-github
                  key: installation-id
            - name: GITHUB_APP_PRIVATE_KEY_PATH
              value: /secrets/github/app.pem
            - name: GOOGLE_APPLICATION_CREDENTIALS
              value: /secrets/gcp/credentials.json
            - name: CLOUD_ML_REGION
              value: us-east5
            - name: ANTHROPIC_VERTEX_PROJECT_ID
              valueFrom:
                secretKeyRef:
                  name: oompa-gcp
                  key: project-id
          volumeMounts:
            - name: github-app-key
              mountPath: /secrets/github
              readOnly: true
            - name: gcp-credentials
              mountPath: /secrets/gcp
              readOnly: true
            - name: work
              mountPath: /work
          resources:
            requests:
              cpu: 500m
              memory: 512Mi
            limits:
              cpu: "2"
              memory: 2Gi
      volumes:
        - name: github-app-key
          secret:
            secretName: github-app-key
        - name: gcp-credentials
          secret:
            secretName: gcp-credentials
        - name: work
          emptyDir: {}
```

## Secrets

Create the required secrets:

```bash
# GitHub App private key
kubectl create secret generic github-app-key \
  --from-file=app.pem=/path/to/private-key.pem \
  -n oompa

# GCP credentials
kubectl create secret generic gcp-credentials \
  --from-file=credentials.json=/path/to/credentials.json \
  -n oompa

# GitHub App IDs
kubectl create secret generic oompa-github \
  --from-literal=app-id=123456 \
  --from-literal=installation-id=78901234 \
  -n oompa

# GCP project
kubectl create secret generic oompa-gcp \
  --from-literal=project-id=my-gcp-project \
  -n oompa
```

## Design Notes

- `strategy.type: Recreate` ensures only one instance runs at a time (oompa uses sequential processing)
- `emptyDir` for `/work` is sufficient since oompa rebuilds state from GitHub on startup
- The container runs as a non-root user and supports OpenShift's random UID assignment
- Git credentials are configured system-wide in the container using the `GH_TOKEN` environment variable
