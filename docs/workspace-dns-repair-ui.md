# Workspace DNS repair UI

The active **Projects & Runtimes** workspace now exposes Cloudflare DNS repair in the UI that users actually open.

- Project rows with a saved domain and Tunnel ID show a compact **修复DNS** action.
- The project MCP configuration dialog shows **修复 DNS** inside the Cloudflare section.
- DNS repair reuses the instance's saved Tunnel UUID and domain, then verifies public DNS through the existing backend repair flow.
- The duplicate repair action was removed from the legacy Instances page, which currently redirects to the workspace.
