from pathlib import Path

path = Path("app/internal/tunnel/cloudflare.go")
text = path.read_text(encoding="utf-8")
old = "\trouteOutput, routeErr := c.ensureDNSRoute(commandCtx, cfg, tunnelID, request.Domain)\n"
new = "\t_, routeErr := c.ensureDNSRoute(commandCtx, cfg, tunnelID, request.Domain)\n"
if old not in text:
    raise SystemExit("expected unused routeOutput assignment not found")
path.write_text(text.replace(old, new, 1), encoding="utf-8")
