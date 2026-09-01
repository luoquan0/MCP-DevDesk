# Cloudflare Tunnel DNS repair

MCP DevDesk now verifies public DNS after configuring a Cloudflare Tunnel hostname.

- DNS routing is bound to the exact Tunnel UUID.
- Older cloudflared builds that do not support `--overwrite-dns` automatically fall back to the compatible route syntax.
- If the hostname is not publicly resolvable after the first route attempt, DevDesk retries once.
- A configured MCP instance exposes a **修复 DNS** action that can repair an existing Tunnel whose hostname record is missing.
- Tunnel configuration is only reported as successful after public DNS resolution is visible; failures include the relevant cloudflared output.
