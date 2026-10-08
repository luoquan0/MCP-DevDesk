#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# A packaged copy lives beside the binaries; the source copy is not runnable.
[[ -x "$ROOT/mcp-devdesk" ]] || { echo 'Run this script from the extracted Linux release package.' >&2; exit 1; }
case "$ROOT" in *[\\\"\$\`%]*|*$'\n'*|*$'\r'*) echo 'Installation path contains characters unsupported by systemd unit quoting.' >&2; exit 1;; esac
emit_unit() {
cat <<EOF
[Unit]
Description=MCP DevDesk Linux Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory="$ROOT"
ExecStart="$ROOT/mcp-devdesk" --root "$ROOT"
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
KillMode=control-group
UMask=0077
NoNewPrivileges=true

[Install]
WantedBy=default.target
EOF
}
if [[ ${1:-} == '--print' ]]; then emit_unit; exit 0; fi
[[ $EUID != 0 ]] || { echo 'Install as the normal account that owns the projects, not root.' >&2; exit 1; }
[[ -f "$ROOT/data/devdesk/config.json" && -f "$ROOT/data/devdesk/secrets.json" ]] || { echo 'Run ./mcp-devdesk --init first.' >&2; exit 1; }
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
mkdir -p -- "$UNIT_DIR"
emit_unit > "$UNIT_DIR/mcp-devdesk.service"
systemctl --user daemon-reload
systemctl --user enable --now mcp-devdesk.service
printf '%s\n' 'Service installed. Status: systemctl --user status mcp-devdesk' 'Logs: journalctl --user -u mcp-devdesk -f' 'To keep the user service after logout, an administrator can enable linger for this account.'
