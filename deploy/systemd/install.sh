#!/bin/bash

set -euo pipefail

INSTANCE_NAME="${1:-}"

if [ -z "$INSTANCE_NAME" ]; then
    echo "Usage: $0 <instance-name>"
    echo ""
    echo "Example: $0 issue-resolver"
    echo "         $0 pr-babysitter"
    echo ""
    echo "This will install oompa@<instance-name>.service"
    exit 1
fi

echo "Installing oompa@${INSTANCE_NAME}.service..."

# Check prerequisites
if ! command -v gh &> /dev/null; then
    echo "Error: gh CLI is not installed or not in PATH"
    echo "Install it from: https://cli.github.com/"
    exit 1
fi

if ! gh auth status &> /dev/null; then
    echo "Error: gh CLI is not authenticated"
    echo "Run: gh auth login"
    exit 1
fi

# Create systemd directory structure
SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
mkdir -p "${SYSTEMD_USER_DIR}"

# Copy service file to user systemd directory
cp oompa@.service "${SYSTEMD_USER_DIR}/"

echo "Service file installed to: ${SYSTEMD_USER_DIR}/oompa@.service"

# Create environment file directory
ENV_DIR="${HOME}/.config/oompa"
mkdir -p "${ENV_DIR}"

ENV_FILE="${ENV_DIR}/${INSTANCE_NAME}.env"

if [ -f "$ENV_FILE" ]; then
    echo "Environment file already exists: ${ENV_FILE}"
    echo "Skipping environment file creation (not overwriting existing config)"
else
    cp env.example "${ENV_FILE}"
    echo "Environment file template created: ${ENV_FILE}"
    echo ""
    echo "⚠️  IMPORTANT: Edit ${ENV_FILE} and configure:"
    echo "   - GitHub authentication (GITHUB_TOKEN or GitHub App credentials)"
    echo "   - Google Cloud Vertex AI settings (CLOUD_ML_REGION, ANTHROPIC_VERTEX_PROJECT_ID)"
    echo "   - Oompa flags (OOMPA_FLAGS)"
fi

# Update the service file to use the user's home directory for the environment file
sed -i "s|/etc/oompa/%i.env|${ENV_DIR}/%i.env|g" "${SYSTEMD_USER_DIR}/oompa@.service"

# Reload systemd
systemctl --user daemon-reload

echo ""
echo "✅ Installation complete!"
echo ""
echo "Next steps:"
echo "  1. Edit the environment file: ${ENV_FILE}"
echo "  2. Start the service:    systemctl --user start oompa@${INSTANCE_NAME}"
echo "  3. Enable on boot:       systemctl --user enable oompa@${INSTANCE_NAME}"
echo "  4. Check status:         systemctl --user status oompa@${INSTANCE_NAME}"
echo "  5. View logs:            journalctl --user -u oompa@${INSTANCE_NAME} -f"
echo ""
echo "To enable lingering (run even when not logged in):"
echo "  loginctl enable-linger $USER"
