package buildinfo

// Version is shared by the desktop manager and the Go MCP core.
const Version = "0.12.16"

// Repository is the default GitHub Releases update source. GitHub Actions may
// still override it at link time for forks or alternate release repositories.
var Repository = "luoquan0/MCP-DevDesk"
