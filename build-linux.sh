#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
cd "$ROOT"
ARCH=$(go env GOHOSTARCH)
case "$ARCH" in amd64|arm64) ;; *) echo 'Only Linux amd64 and arm64 are supported.' >&2; exit 1;; esac
[[ $(go env GOHOSTOS) == linux ]] || { echo 'Run on a native Linux host.' >&2; exit 1; }
STAGE="$ROOT/dist/MCP-DevDesk-Linux-$ARCH"
mkdir -p "$ROOT/dist"
rm -rf -- "$STAGE"
mkdir -p -- "$STAGE"
(
 cd app
 go test -mod=vendor -count=1 -timeout=120s ./...
 CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -mod=vendor -trimpath -ldflags='-s -w' -o "$STAGE/mcp-devdesk" ./cmd/mcp-devdesk-server
 CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -mod=vendor -trimpath -ldflags='-s -w' -o "$STAGE/mcp-core" ./cmd/mcp-core
 CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -mod=vendor -trimpath -ldflags='-s -w' -o "$STAGE/devdeskctl" ./cmd/devdeskctl
)
python3 - "$ARCH" "$STAGE" <<'PY'
import hashlib, json, pathlib, re, subprocess, sys, urllib.request
arch, stage = sys.argv[1], pathlib.Path(sys.argv[2])
pin = json.loads(pathlib.Path('tools/cloudflared-release.json').read_text())
version = pin['version']
if not re.fullmatch(r'\d{4}\.\d+\.\d+', version):
    raise SystemExit('Invalid cloudflared version pin')
asset_name = 'cloudflared-linux-' + arch
release = json.loads(subprocess.check_output(['gh', 'api', 'repos/cloudflare/cloudflared/releases/tags/' + version]))
asset = next(a for a in release['assets'] if a['name'] == asset_name)
digest = asset.get('digest', '')
if not re.fullmatch(r'sha256:[0-9a-fA-F]{64}', digest):
    raise SystemExit('Official cloudflared asset has no SHA256 digest; refusing unverified download')
expected = digest.split(':')[1].lower()
url = 'https://github.com/cloudflare/cloudflared/releases/download/' + version + '/' + asset_name
if asset['browser_download_url'] != url:
    raise SystemExit('Unexpected cloudflared download source')
hash_value = hashlib.sha256()
with urllib.request.urlopen(url, timeout=120) as response, (stage/'cloudflared').open('wb') as out:
    count = 0
    while chunk := response.read(1024*1024):
        count += len(chunk)
        if count > 150*1024*1024:
            raise SystemExit('cloudflared asset too large')
        hash_value.update(chunk); out.write(chunk)
if hash_value.hexdigest() != expected:
    raise SystemExit('cloudflared SHA256 mismatch')
(stage/'cloudflared').chmod(0o755)
text = subprocess.check_output([str(stage/'cloudflared'),'--version'], text=True).strip()
if 'version '+version not in text:
    raise SystemExit('cloudflared version verification failed: '+text)
(stage/'cloudflared-source.json').write_text(json.dumps({'version':version,'asset':asset_name,'url':url,'sha256':expected},indent=2)+'\n')
print('Verified bundled ' + text)
PY
cp docs/LINUX.md "$STAGE/README.md"
cp tools/linux/install-service.sh tools/linux/install-linux.sh "$STAGE/"
chmod 0755 "$STAGE/mcp-devdesk" "$STAGE/mcp-core" "$STAGE/devdeskctl" "$STAGE/cloudflared" "$STAGE/"*.sh
(
 cd "$STAGE"
 sha256sum mcp-devdesk mcp-core devdeskctl cloudflared cloudflared-source.json README.md install-service.sh install-linux.sh > SHA256SUMS
 sha256sum -c SHA256SUMS
)
python3 tools/smoke-linux.py "$STAGE"
tar -czf "$ROOT/dist/MCP-DevDesk-Linux-$ARCH.tar.gz" -C "$ROOT/dist" "MCP-DevDesk-Linux-$ARCH"
(
 cd "$ROOT/dist"
 sha256sum "MCP-DevDesk-Linux-$ARCH.tar.gz" > "MCP-DevDesk-Linux-$ARCH.tar.gz.sha256"
)
echo "Linux $ARCH package verified: dist/MCP-DevDesk-Linux-$ARCH.tar.gz"
