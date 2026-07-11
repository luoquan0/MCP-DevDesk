package process

import (
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"mcp-devdesk/internal/model"
)

func parseCloudflaredCommandLine(commandLine string) model.TunnelProcess {
	args := splitWindowsCommandLine(commandLine)
	result := model.TunnelProcess{CommandLine: redactCloudflaredArguments(args)}
	if len(args) == 0 {
		return result
	}
	result.ProcessPath = strings.Trim(args[0], `"`)

	runIndex := -1
	for index, argument := range args {
		lower := strings.ToLower(argument)
		switch {
		case lower == "run" && index > 0 && strings.EqualFold(args[index-1], "tunnel"):
			runIndex = index
		case lower == "--url" && index+1 < len(args):
			result.LocalURL = args[index+1]
		case strings.HasPrefix(lower, "--url="):
			result.LocalURL = argument[len("--url="):]
		case lower == "--credentials-file" && index+1 < len(args):
			result.CredentialsPath = args[index+1]
		case strings.HasPrefix(lower, "--credentials-file="):
			result.CredentialsPath = argument[len("--credentials-file="):]
		}
	}

	if result.CredentialsPath != "" {
		base := filepath.Base(strings.Trim(result.CredentialsPath, `"`))
		result.TunnelID = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if runIndex >= 0 {
		result.TunnelName = findTunnelRunName(args[runIndex+1:])
	}
	populateLocalTarget(&result)
	return result
}

func isCloudflaredTunnelCommand(commandLine string) bool {
	args := splitWindowsCommandLine(commandLine)
	tunnelIndex := -1
	for index, argument := range args {
		if strings.EqualFold(argument, "tunnel") {
			tunnelIndex = index
			break
		}
	}
	if tunnelIndex < 0 {
		return false
	}
	for _, argument := range args[tunnelIndex+1:] {
		lower := strings.ToLower(argument)
		if lower == "run" || lower == "--url" || strings.HasPrefix(lower, "--url=") {
			return true
		}
	}
	return false
}

func redactCloudflaredArguments(args []string) string {
	redacted := append([]string(nil), args...)
	for index := range redacted {
		lower := strings.ToLower(redacted[index])
		if lower == "--token" && index+1 < len(redacted) {
			redacted[index+1] = "***"
			continue
		}
		if strings.HasPrefix(lower, "--token=") {
			redacted[index] = "--token=***"
		}
	}
	return strings.Join(redacted, " ")
}

func findTunnelRunName(args []string) string {
	valueFlags := map[string]bool{
		"--credentials-file": true,
		"--protocol":         true,
		"--url":              true,
		"--token":            true,
		"--config":           true,
		"--loglevel":         true,
		"--logfile":          true,
		"--metrics":          true,
		"--edge-ip-version":  true,
	}
	name := ""
	for index := 0; index < len(args); index++ {
		argument := args[index]
		lower := strings.ToLower(argument)
		if strings.HasPrefix(argument, "-") {
			if valueFlags[lower] && index+1 < len(args) {
				index++
			}
			continue
		}
		name = argument
	}
	return strings.Trim(name, `"`)
}

func populateLocalTarget(process *model.TunnelProcess) {
	if process.LocalURL == "" {
		return
	}
	parsed, err := url.Parse(strings.Trim(process.LocalURL, `"`))
	if err != nil {
		return
	}
	process.LocalHost = parsed.Hostname()
	portText := parsed.Port()
	if portText == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http":
			process.LocalPort = 80
		case "https":
			process.LocalPort = 443
		}
		return
	}
	process.LocalPort, _ = strconv.Atoi(portText)
	if process.LocalHost == "" {
		host, port, splitErr := net.SplitHostPort(parsed.Host)
		if splitErr == nil {
			process.LocalHost = host
			process.LocalPort, _ = strconv.Atoi(port)
		}
	}
}

func splitWindowsCommandLine(commandLine string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	backslashes := 0
	flushBackslashes := func(count int) {
		for index := 0; index < count; index++ {
			current.WriteByte('\\')
		}
	}
	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}

	for _, character := range commandLine {
		switch character {
		case '\\':
			backslashes++
		case '"':
			flushBackslashes(backslashes / 2)
			if backslashes%2 == 1 {
				current.WriteRune('"')
			} else {
				inQuotes = !inQuotes
			}
			backslashes = 0
		case ' ', '\t':
			flushBackslashes(backslashes)
			backslashes = 0
			if inQuotes {
				current.WriteRune(character)
			} else {
				flush()
			}
		default:
			flushBackslashes(backslashes)
			backslashes = 0
			current.WriteRune(character)
		}
	}
	flushBackslashes(backslashes)
	flush()
	return args
}
