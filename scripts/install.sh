#!/usr/bin/env bash
#
# install.sh — install apiproxy to /opt/apiproxy and configure systemd auto-start.
#
# This script is designed to be bundled inside the distribution tarball.
# It finds the binary and config files relative to its own location.
#
# Usage:
#   sudo ./install.sh                              # install with auto-generated admin password
#   sudo ./install.sh -u admin -p mypassword       # set admin credentials
#   sudo ./install.sh -d /opt/apiproxy             # install to custom directory
#   sudo ./install.sh --uninstall                  # remove service and installation directory
#   sudo ./install.sh --upgrade                    # stop service, reinstall, start service
#
# Environment variables (fallback when -u/-p not given):
#   APIPROXY_ADMIN_USER   admin username (default: admin)
#   APIPROXY_ADMIN_PASS   admin password (default: auto-generated)
#
set -euo pipefail

# ── constants ─────────────────────────────────────────────────────────────────
INSTALL_DIR="/usr/local/apiproxy"
SERVICE_FILE="/etc/systemd/system/apiproxy.service"
SERVICE_NAME="apiproxy"

# Script's own directory (works when bundled in tarball or in repo).
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ── defaults ──────────────────────────────────────────────────────────────────
ADMIN_USER="${APIPROXY_ADMIN_USER:-admin}"
ADMIN_PASS="${APIPROXY_ADMIN_PASS:-}"
UNINSTALL=false
UPGRADE=false

# ── colour helpers ────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; }
step()  { echo -e "${BLUE}[STEP]${NC}  $*"; }

# ── usage ─────────────────────────────────────────────────────────────────────
usage() {
  cat <<EOF
Usage: $0 [OPTIONS]

Install apiproxy to ${INSTALL_DIR} and configure systemd auto-start.

Options:
  -d, --dir PATH        Installation directory (default: /usr/local/apiproxy)
  -u, --user NAME       Admin username (default: admin)
  -p, --pass PASSWORD   Admin password (default: auto-generated)
  --upgrade             Upgrade an existing installation (stop → install → start)
  --uninstall           Remove apiproxy service and installation directory
  -h, --help            Show this help message

Environment:
  APIPROXY_ADMIN_USER   Fallback admin username
  APIPROXY_ADMIN_PASS   Fallback admin password

Examples:
  sudo ./install.sh                      # fresh install
  sudo ./install.sh -d /opt/apiproxy     # install to custom directory
  sudo ./install.sh -u admin -p secret   # install with custom credentials
  sudo ./install.sh --upgrade            # upgrade existing installation
  sudo ./install.sh --uninstall          # remove everything
EOF
  exit 0
}

# ── parse args ────────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--dir)       INSTALL_DIR="$2"; shift 2 ;;
    -u|--user)      ADMIN_USER="$2"; shift 2 ;;
    -p|--pass)      ADMIN_PASS="$2"; shift 2 ;;
    --upgrade)      UPGRADE=true; shift ;;
    --uninstall)    UNINSTALL=true; shift ;;
    -h|--help)      usage ;;
    *)              error "Unknown option: $1"; usage ;;
  esac
done

# ── pre-flight checks ─────────────────────────────────────────────────────────
if [[ "$(id -u)" -ne 0 ]]; then
  error "This script must be run as root (use sudo)."
  exit 1
fi

# ── uninstall ─────────────────────────────────────────────────────────────────
do_uninstall() {
  step "Uninstalling apiproxy..."

  if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    info "Stopping service..."
    systemctl stop "$SERVICE_NAME"
  fi

  if systemctl is-enabled --quiet "$SERVICE_NAME" 2>/dev/null; then
    info "Disabling service..."
    systemctl disable "$SERVICE_NAME"
  fi

  if [[ -f "$SERVICE_FILE" ]]; then
    info "Removing service file: $SERVICE_FILE"
    rm -f "$SERVICE_FILE"
    systemctl daemon-reload
  fi

  if [[ -d "$INSTALL_DIR" ]]; then
    info "Removing installation directory: $INSTALL_DIR"
    rm -rf "$INSTALL_DIR"
  fi

  info "Uninstall complete."
  exit 0
}

if [[ "$UNINSTALL" == true ]]; then
  do_uninstall
fi

# ── generate password if not provided ─────────────────────────────────────────
generate_password() {
  if command -v openssl &>/dev/null; then
    openssl rand -base64 24 | tr -d '\n'
  elif command -v uuidgen &>/dev/null; then
    uuidgen
  else
    echo "apiproxy$(date +%s)"
  fi
}

# ── create systemd service file ───────────────────────────────────────────────
write_service_file() {
  step "Creating systemd service file: $SERVICE_FILE"

  cat > "$SERVICE_FILE" <<SERVICEEOF
[Unit]
Description=API Proxy Service
Documentation=https://github.com/wy7980/apiproxy
After=network.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/apiproxy --config ${INSTALL_DIR}/configs/apiproxy.yaml
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}
Environment=APIPROXY_ADMIN_USER=${ADMIN_USER}
Environment=APIPROXY_ADMIN_PASS=${ADMIN_PASS}

[Install]
WantedBy=multi-user.target
SERVICEEOF

  info "Service file created."
}

# ── install from local files ──────────────────────────────────────────────────
do_install() {
  step "Installing apiproxy to ${INSTALL_DIR}..."

  # Validate required files exist alongside the script.
  if [[ ! -f "$SCRIPT_DIR/apiproxy" ]]; then
    error "Binary not found: $SCRIPT_DIR/apiproxy"
    error "Make sure install.sh is in the same directory as the apiproxy binary."
    exit 1
  fi
  if [[ ! -f "$SCRIPT_DIR/configs/apiproxy.yaml" ]]; then
    error "Config not found: $SCRIPT_DIR/configs/apiproxy.yaml"
    exit 1
  fi

  # Create installation directory.
  mkdir -p "$INSTALL_DIR"

  # Copy binary.
  info "Copying binary..."
  cp "$SCRIPT_DIR/apiproxy" "$INSTALL_DIR/apiproxy"
  chmod +x "$INSTALL_DIR/apiproxy"

  # Copy configs.
  info "Copying configs..."
  cp -r "$SCRIPT_DIR/configs" "$INSTALL_DIR/configs"

  # Copy README if present.
  if [[ -f "$SCRIPT_DIR/README.md" ]]; then
    cp "$SCRIPT_DIR/README.md" "$INSTALL_DIR/README.md"
  fi

  # Create runtime directories.
  mkdir -p "$INSTALL_DIR/logs" "$INSTALL_DIR/data"

  # Update config paths to absolute paths.
  local config_file="$INSTALL_DIR/configs/apiproxy.yaml"
  if [[ -f "$config_file" ]]; then
    sed -i "s|path: \"data/apiproxy.db\"|path: \"${INSTALL_DIR}/data/apiproxy.db\"|" "$config_file"
    sed -i "s|dir: \"logs\"|dir: \"${INSTALL_DIR}/logs\"|" "$config_file"
    info "Config paths updated to absolute paths."
  fi

  info "apiproxy installed to ${INSTALL_DIR}."
}

# ── configure systemd ─────────────────────────────────────────────────────────
do_systemd() {
  step "Configuring systemd..."

  write_service_file

  info "Reloading systemd daemon..."
  systemctl daemon-reload

  info "Enabling service for auto-start..."
  systemctl enable "$SERVICE_NAME"

  info "Starting service..."
  systemctl start "$SERVICE_NAME"

  # Wait a moment and check status.
  sleep 2
  if systemctl is-active --quiet "$SERVICE_NAME"; then
    info "Service is running."
  else
    warn "Service may not have started. Check: systemctl status $SERVICE_NAME"
    warn "View logs: journalctl -u $SERVICE_NAME --no-pager -n 50"
  fi
}

# ── upgrade ───────────────────────────────────────────────────────────────────
do_upgrade() {
  step "Upgrading apiproxy..."

  if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
    info "Stopping service..."
    systemctl stop "$SERVICE_NAME"
  fi

  # Remove old binary and configs, keep logs and data.
  rm -f "$INSTALL_DIR/apiproxy"
  rm -rf "$INSTALL_DIR/configs"
  rm -f "$INSTALL_DIR/README.md"

  do_install

  info "Starting service..."
  systemctl start "$SERVICE_NAME"

  sleep 2
  if systemctl is-active --quiet "$SERVICE_NAME"; then
    info "Upgrade complete. Service is running."
  else
    warn "Service may not have started. Check: systemctl status $SERVICE_NAME"
    warn "View logs: journalctl -u $SERVICE_NAME --no-pager -n 50"
  fi
}

# ── print summary ─────────────────────────────────────────────────────────────
print_summary() {
  cat <<EOF

${GREEN}══════════════════════════════════════════════════════════════${NC}
${GREEN}  apiproxy installation complete${NC}
${GREEN}══════════════════════════════════════════════════════════════${NC}

  Install:    ${INSTALL_DIR}
  Binary:     ${INSTALL_DIR}/apiproxy
  Config:     ${INSTALL_DIR}/configs/apiproxy.yaml
  Logs:       ${INSTALL_DIR}/logs/
  Data:       ${INSTALL_DIR}/data/

  Admin URL:  http://<host>:8081
  Admin user: ${ADMIN_USER}
  Admin pass: ${ADMIN_PASS}

  Service:    ${SERVICE_NAME}
  Status:     systemctl status ${SERVICE_NAME}
  Logs:       journalctl -u ${SERVICE_NAME} -f

${GREEN}══════════════════════════════════════════════════════════════${NC}
EOF
}

# ═══════════════════════════════════════════════════════════════════════════════
#  MAIN
# ═══════════════════════════════════════════════════════════════════════════════

# Auto-generate password if not provided.
if [[ -z "$ADMIN_PASS" ]]; then
  ADMIN_PASS="$(generate_password)"
  info "Auto-generated admin password: ${ADMIN_PASS}"
  warn "Please save this password or set APIPROXY_ADMIN_PASS for repeatability."
fi

if [[ "$UPGRADE" == true ]]; then
  do_upgrade
else
  do_install
  do_systemd
fi

print_summary
