# MCP DevDesk Linux Server — v0.12.38-linux.1

This is the first **Linux preview**, developed on `feature/linux-server` from stable Windows `v0.12.37`. It is not a Windows update and must not replace the Windows latest stable Release. Windows `main` is unchanged.

## Download and start / 下载与启动

Choose `MCP-DevDesk-Linux-amd64.tar.gz` for x86-64 Intel/AMD, or `MCP-DevDesk-Linux-arm64.tar.gz` for AArch64. These packages contain native Linux programs, the embedded Vue interface, a verified Cloudflare runtime, and installation helpers. Node.js, Go and a graphical desktop are not needed to run the manager. Install the development tools needed by your own projects separately (Git, Go, Node/npm, Rust, Python, .NET, etc.).

```bash
sha256sum -c MCP-DevDesk-Linux-amd64.tar.gz.sha256
tar -xzf MCP-DevDesk-Linux-amd64.tar.gz
cd MCP-DevDesk-Linux-amd64
./mcp-devdesk --init --lan
./mcp-devdesk
```

`--init` creates a private `data/devdesk` directory, chooses the Go core, enables password-protected Web Control on port **17861**, and prints a randomly generated web password once. Save this password. There is no shared default password. Initial workspace: `workspace/` inside the installation directory. The browser directory picker can select other directories accessible to the service account.

On your private LAN, open `http://LINUX_PRIVATE_IP:17861/` and sign in. `--web-port 17862` may be supplied during initialization to choose another port. Allow that port in the host firewall **only for trusted LAN clients**.

For an Internet VPS, do not expose either management port directly. Initialize without `--lan`, keep loopback-only listening, and open an SSH tunnel from your own computer:

```bash
ssh -N -L 17861:127.0.0.1:17861 user@server
```

Then open `http://127.0.0.1:17861/` on that computer. The existing web policy accepts only loopback/private client addresses and loopback/private-IP Host headers; arbitrary public-IP/domain management access is intentionally not enabled. Internal port **17860** is a local-only management API, not a public browser endpoint. LAN HTTP is not encrypted; use SSH forwarding on untrusted networks.

## Background service / 后台运行

Run as a normal account that owns the projects, not root. After initialization:

```bash
./install-service.sh
systemctl --user status mcp-devdesk
journalctl --user -u mcp-devdesk -f
```

This installs a systemd **user** service with automatic restart, a restrictive umask and control-group shutdown. On systems without a user systemd session, use the foreground command or configure the supplied unit for your supervisor. To keep the user service after logout / start it at boot, an administrator can run `sudo loginctl enable-linger USERNAME`. `./install-service.sh --print` displays the generated unit without installing it.

## Passwords and sensitive settings / 密码和敏感设置

Stop the service and reset the web password when necessary:

```bash
systemctl --user stop mcp-devdesk
./mcp-devdesk --reset-web-password
systemctl --user start mcp-devdesk
```

For automated initialization/reset, set `MCP_DEVDESK_WEB_PASSWORD` in the process environment (8–256 characters); it is not printed. Do not put passwords in command-line arguments, scripts committed to Git, or unit files readable by other users.

The browser API deliberately does not expose `/api/secrets`. To retrieve the MCP OAuth owner password and client credentials locally, use `./mcp-devdesk --show-mcp-credentials` from a trusted terminal. Keep its output private. Other sensitive configuration can be managed through the loopback-only management API from the server account or an authenticated SSH session; it is not available to unauthenticated remote callers.

Linux secrets use **AES-256-GCM**, random nonces and an owner-only 32-byte master key. For the server the default key is `data/devdesk/master.key` (0600), outside the default project workspace. `MCP_DEVDESK_KEY_FILE` can point to another private key file and must be consistent for the manager and its cores. A standalone `mcp-core` without that setting uses the user's config directory, `mcp-devdesk/master.key`. Incorrect permissions, symlink keys, corrupted ciphertext and missing decryption keys fail closed. Windows DPAPI files are not directly portable to Linux.

Back up the master key **together with** encrypted data; losing it makes the secrets unreadable. This is file-based encryption, not TPM/keyring isolation: it does not defend against root, the same account or an attacker who steals both key and ciphertext. Use dedicated OS accounts and do not grant MCP tools access to the application's private data directory. Command permission modes are not an OS sandbox.

## Included and intentionally excluded / 功能范围

Included: the shared Vue Web Control, projects and instances, Go MCP Core, OAuth, logs, project validation (`validate_project`, `checks_run`), terminal sessions (`exec_command`, `read_output`, `write_stdin`, `kill_session`), lexical code navigation, Linux TCP ownership checks and Cloudflare tunnel process inventory/control.

The Cloudflare runtime is pinned to the same version recorded in `tools/cloudflared-release.json`. Each Linux build downloads the appropriate official architecture asset, requires its official SHA256 digest, verifies the bytes and `--version`, and records provenance in `cloudflared-source.json`. Cloudflare's own update endpoint selects the Linux asset. Cloudflare login/configuration requires the user's own authorization; no account credentials or preconfigured tunnels are shipped.

Excluded: Windows WebView2, tray, registry startup, Windows legacy `.exe` core, Screen Vision on Linux, Persistent LSP, and the 0.13 AI Task/automatic Worktree/review architecture. Linux Screen Vision is not implemented; leave it disabled. The existing Windows build remains separate. No new UI fork is introduced; desktop-only controls may report unsupported on Linux.

## Updating Linux / 升级 Linux 版本

Automatic in-app binary installation is deliberately disabled in this first preview so it cannot download or run a Windows updater. Stop the service, extract the **new release into a different directory**, then run the new package's helper:

```bash
systemctl --user stop mcp-devdesk
# In the freshly extracted NEW package:
./install-linux.sh /path/to/existing/MCP-DevDesk-Linux-amd64
systemctl --user start mcp-devdesk
```

The helper verifies the new package's SHA256 manifest, refuses to update a running server, replaces application files with rollback on failure, and leaves existing `data/`, master key, projects and **cloudflared** alone. It installs the bundled cloudflared only if absent. Do not unpack a new tarball directly over an existing installation: a generic tar extraction bypasses this preservation logic. `cloudflared-source.json` describes the bundled recovery copy, not necessarily the independently-updated runtime currently installed.

## Building and validation / 构建与验证

All distributable builds run in GitHub Actions. Native Ubuntu amd64 and arm64 jobs build the frontend, run `go test -mod=vendor -count=1 -timeout=120s ./...`, build static Go programs and run `tools/smoke-linux.py` against the real binaries. A separate Windows job runs the existing full `build.ps1 -Arch amd64 -RunTests` gate. Linux publishing requires every mandatory job to pass.

For development on a native Linux host: build `frontend` with `npm ci && npm run build`, then run `bash build-linux.sh`. Building also requires Python 3, GitHub CLI (`gh`), network access to official Cloudflare releases and the Go version in `app/go.mod`. Runtime tarballs include SHA256SUMS internally and a `.sha256` file externally. They are checksum-verified but do not have an independent release signature.
