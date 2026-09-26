#!/usr/bin/env bash
# Run from a newly extracted release, after stopping the existing service.
set -euo pipefail
SOURCE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
[[ $# == 1 && -n $1 ]] || { echo 'Usage: ./install-linux.sh /path/to/existing-or-new-installation' >&2; exit 1; }
[[ -f "$SOURCE/SHA256SUMS" && -x "$SOURCE/mcp-devdesk" ]] || { echo 'This script must be run from a Linux release package.' >&2; exit 1; }
(cd "$SOURCE" && sha256sum --strict -c SHA256SUMS)
mkdir -p -- "$1"
DEST=$(cd -- "$1" && pwd -P)
[[ $DEST != '/' && $DEST != "$SOURCE" ]] || { echo 'Use a separate target directory, not / or the extracted source directory.' >&2; exit 1; }
command -v flock >/dev/null || { echo 'flock (util-linux) is required.' >&2; exit 1; }
mkdir -p -m 700 -- "$DEST/data/devdesk"
exec 9>"$DEST/data/devdesk/server.lock"
flock -n 9 || { echo 'Stop the existing MCP DevDesk service before upgrading.' >&2; exit 1; }
TEMP=$(mktemp -d "$DEST/.linux-install.XXXXXXXX")
mkdir "$TEMP/old" "$TEMP/new"
FILES=(mcp-devdesk mcp-core devdeskctl README.md cloudflared-source.json install-service.sh install-linux.sh)
CHANGED=()
rollback() {
 local rc=$?
 if ((rc != 0)); then
  for ((i=${#CHANGED[@]}-1;i>=0;i--)); do
   name=${CHANGED[i]}
   if [[ -e "$TEMP/old/$name" ]]; then mv -f -- "$TEMP/old/$name" "$DEST/$name"; else rm -f -- "$DEST/$name"; fi
  done
  echo 'Installation failed; replaced application files were rolled back.' >&2
 fi
 rm -rf -- "$TEMP"
 exit "$rc"
}
trap rollback EXIT
for name in "${FILES[@]}"; do
 [[ ! -d "$DEST/$name" ]] || { echo "Target is a directory: $name" >&2; exit 1; }
 cp -p -- "$SOURCE/$name" "$TEMP/new/$name"
 if [[ -e "$DEST/$name" ]]; then cp -p -- "$DEST/$name" "$TEMP/old/$name"; fi
done
for name in "${FILES[@]}"; do
 CHANGED+=("$name")
 mv -f -- "$TEMP/new/$name" "$DEST/$name"
done
# Never overwrite an independently updated cloudflared (even if it is older).
if [[ ! -e "$DEST/cloudflared" && ! -L "$DEST/cloudflared" ]]; then
 cp -p -- "$SOURCE/cloudflared" "$TEMP/new/cloudflared"
 ln -- "$TEMP/new/cloudflared" "$DEST/cloudflared"
 echo 'Installed missing cloudflared runtime.'
else
 echo 'Preserved existing cloudflared runtime.'
fi
printf '%s\n' 'Installed application files; existing data/, master key and workspace were not replaced.' 'New installation: run ./mcp-devdesk --init from the target directory.' 'Existing installation: restart the user service.'
