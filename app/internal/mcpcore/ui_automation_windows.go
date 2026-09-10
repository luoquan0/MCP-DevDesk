//go:build windows

package mcpcore

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const uiAutomationPowerShell = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
$handle = [IntPtr]([Int64]$env:MCP_UIA_HWND)
$maxDepth = [int]$env:MCP_UIA_DEPTH
$maxNodes = [int]$env:MCP_UIA_NODES
$root = [System.Windows.Automation.AutomationElement]::FromHandle($handle)
if ($null -eq $root) { throw 'UI Automation root is unavailable' }
$walker = [System.Windows.Automation.TreeWalker]::ControlViewWalker
$items = New-Object System.Collections.Generic.List[object]
$script:truncated = $false
function SafeText([object]$value) {
  if ($null -eq $value) { return '' }
  $text = [string]$value
  if ($text.Length -gt 512) { return $text.Substring(0, 512) }
  return $text
}
function AddNode([System.Windows.Automation.AutomationElement]$element, [int]$depth) {
  if ($null -eq $element) { return }
  if ($items.Count -ge $maxNodes) { $script:truncated = $true; return }
  try { $c = $element.Current } catch { return }
  $r = $c.BoundingRectangle
  $value = ''
  $isPassword = [bool]$c.IsPassword
  if (-not $isPassword) {
    try {
      $pattern = $null
      if ($element.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern, [ref]$pattern)) {
        $value = SafeText $pattern.Current.Value
      }
    } catch {}
  }
  $controlType = ''
  try { $controlType = SafeText $c.ControlType.ProgrammaticName.Replace('ControlType.', '') } catch {}
  $items.Add([pscustomobject]@{
    depth = $depth
    name = SafeText $c.Name
    automationId = SafeText $c.AutomationId
    controlType = $controlType
    className = SafeText $c.ClassName
    frameworkId = SafeText $c.FrameworkId
    enabled = [bool]$c.IsEnabled
    offscreen = [bool]$c.IsOffscreen
    password = $isPassword
    bounds = [pscustomobject]@{ x=[int]$r.X; y=[int]$r.Y; width=[int]$r.Width; height=[int]$r.Height }
    value = $value
  }) | Out-Null
  if ($depth -ge $maxDepth) { return }
  try { $child = $walker.GetFirstChild($element) } catch { $child = $null }
  while ($null -ne $child) {
    AddNode $child ($depth + 1)
    if ($items.Count -ge $maxNodes) { $script:truncated = $true; break }
    try { $child = $walker.GetNextSibling($child) } catch { break }
  }
}
AddNode $root 0
[pscustomobject]@{ nodes=@($items); truncated=[bool]$script:truncated } | ConvertTo-Json -Depth 8 -Compress`

type uiAutomationPayload struct {
	Nodes     []uiAutomationNode `json:"nodes"`
	Truncated bool               `json:"truncated"`
}

func platformReadUIAutomationTree(window screenWindow, maxDepth, maxNodes int) ([]uiAutomationNode, bool, error) {
	if window.Handle == 0 {
		return nil, false, errors.New("UI Automation target window is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShellCommand(uiAutomationPowerShell))
	cmd.Env = append(commandEnvironment(nil, false),
		"MCP_UIA_HWND="+strconv.FormatUint(uint64(window.Handle), 10),
		"MCP_UIA_DEPTH="+strconv.Itoa(maxDepth),
		"MCP_UIA_NODES="+strconv.Itoa(maxNodes),
	)
	configureCommand(cmd)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, false, errors.New("Windows UI Automation read timed out")
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 1200 {
			message = message[:1200] + "..."
		}
		return nil, false, fmt.Errorf("Windows UI Automation read failed: %w: %s", err, message)
	}
	var payload uiAutomationPayload
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil, false, fmt.Errorf("decode Windows UI Automation tree: %w", err)
	}
	return payload.Nodes, payload.Truncated, nil
}

func encodePowerShellCommand(script string) string {
	encoded := utf16.Encode([]rune(script))
	bytes := make([]byte, len(encoded)*2)
	for index, value := range encoded {
		binary.LittleEndian.PutUint16(bytes[index*2:], value)
	}
	return base64.StdEncoding.EncodeToString(bytes)
}
