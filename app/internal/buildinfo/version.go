package buildinfo

// Version is shared by the desktop manager and the Go MCP core.
const Version = "0.12.8"

// Repository is injected for GitHub release builds with -ldflags -X.
// Local source builds leave it empty and can configure the update source in Settings.
var Repository = ""
