//go:build windows

package mcpcore

import (
	"bytes"
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

const uiAutomationPowerShellHelpers = `function SafeText {
  param([AllowNull()][object]$value)
  if ($null -eq $value) { return '' }
  try { $text = "$value" } catch { return '' }
  if ($null -eq $text) { return '' }
  if ($text.Length -gt 512) { return $text.Substring(0, 512) }
  return $text
}
function SafeInt {
  param([AllowNull()][object]$value)
  if ($null -eq $value) { return 0 }
  try {
    $number = [double]$value
    if ([double]::IsNaN($number) -or [double]::IsInfinity($number)) { return 0 }
    if ($number -gt [int]::MaxValue) { return [int]::MaxValue }
    if ($number -lt [int]::MinValue) { return [int]::MinValue }
    return [int]$number
  } catch { return 0 }
}`

const uiAutomationPowerShellEncoding = `$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom
`

const uiAutomationPowerShell = `$ErrorActionPreference = 'Stop'
` + uiAutomationPowerShellEncoding + `Add-Type -AssemblyName UIAutomationClient
$handle = [IntPtr]([Int64]$env:MCP_UIA_HWND)
$maxDepth = [int]$env:MCP_UIA_DEPTH
$maxNodes = [int]$env:MCP_UIA_NODES
$root = [System.Windows.Automation.AutomationElement]::FromHandle($handle)
if ($null -eq $root) { throw 'UI Automation root is unavailable' }
$walker = [System.Windows.Automation.TreeWalker]::ControlViewWalker
$items = New-Object System.Collections.Generic.List[object]
$script:truncated = $false
` + uiAutomationPowerShellHelpers + `
function AddNode([System.Windows.Automation.AutomationElement]$element, [int]$depth) {
  if ($null -eq $element) { return }
  if ($items.Count -ge $maxNodes) { $script:truncated = $true; return }
  try { $c = $element.Current } catch { return }

  $r = $null
  try { $r = $c.BoundingRectangle } catch {}

  $isPassword = $false
  try { $isPassword = [bool]$c.IsPassword } catch {}
  $value = ''
  if (-not $isPassword) {
    try {
      $pattern = $null
      if ($element.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern, [ref]$pattern)) {
        $value = SafeText ($pattern.Current.Value)
      }
    } catch {}
  }

  $name = ''
  $automationId = ''
  $controlType = ''
  $className = ''
  $frameworkId = ''
  try { $name = SafeText ($c.Name) } catch {}
  try { $automationId = SafeText ($c.AutomationId) } catch {}
  try {
    $programmaticName = SafeText ($c.ControlType.ProgrammaticName)
    if ($programmaticName.StartsWith('ControlType.')) {
      $controlType = $programmaticName.Substring(12)
    } else {
      $controlType = $programmaticName
    }
  } catch {}
  try { $className = SafeText ($c.ClassName) } catch {}
  try { $frameworkId = SafeText ($c.FrameworkId) } catch {}

  $enabled = $false
  $offscreen = $false
  try { $enabled = [bool]$c.IsEnabled } catch {}
  try { $offscreen = [bool]$c.IsOffscreen } catch {}

  $x = 0
  $y = 0
  $width = 0
  $height = 0
  if ($null -ne $r) {
    $x = SafeInt ($r.X)
    $y = SafeInt ($r.Y)
    $width = SafeInt ($r.Width)
    $height = SafeInt ($r.Height)
  }

  $items.Add([pscustomobject]@{
    depth = $depth
    name = $name
    automationId = $automationId
    controlType = $controlType
    className = $className
    frameworkId = $frameworkId
    enabled = $enabled
    offscreen = $offscreen
    password = $isPassword
    bounds = [pscustomobject]@{ x=$x; y=$y; width=$width; height=$height }
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
$nodeArray = $items.ToArray()
[pscustomobject]@{ nodes=$nodeArray; truncated=[bool]$script:truncated } | ConvertTo-Json -Depth 8 -Compress`

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
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, false, errors.New("Windows UI Automation read timed out")
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if len(message) > 1200 {
			message = message[:1200] + "..."
		}
		return nil, false, fmt.Errorf("Windows UI Automation read failed: %w: %s", err, message)
	}
	var payload uiAutomationPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
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
