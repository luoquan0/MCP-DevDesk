package mcpcore

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultScreenMaxWidth = 1920
	minScreenMaxWidth     = 320
	maxScreenMaxWidth     = 4096
	maxScreenWindows      = 200
)

type screenRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type screenWindow struct {
	ID          string     `json:"id"`
	Handle      uintptr    `json:"-"`
	Title       string     `json:"title"`
	ProcessID   uint32     `json:"processId"`
	ProcessName string     `json:"processName,omitempty"`
	Bounds      screenRect `json:"bounds"`
	Active      bool       `json:"active"`
}

type screenCaptureFrame struct {
	Image  *image.NRGBA
	Bounds screenRect
	Method string
}

type screenListArgs struct {
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type screenWindowArgs struct {
	Window   string `json:"window"`
	MaxWidth int    `json:"maxWidth,omitempty"`
}

type screenCaptureArgs struct {
	MaxWidth int `json:"maxWidth,omitempty"`
}

func screenTools() []Tool {
	captureProperties := map[string]any{
		"maxWidth": map[string]any{
			"type":        "integer",
			"minimum":     minScreenMaxWidth,
			"maximum":     maxScreenMaxWidth,
			"description": "Maximum returned image width. The capture is downscaled in memory when needed. Defaults to 1920.",
		},
	}
	return []Tool{
		{
			Name:        "screen_list_windows",
			Title:       "List Visible Windows",
			Description: "List visible top-level Windows application windows. Screen Vision is explicit opt-in and this tool never starts continuous recording.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "maxLength": 200, "description": "Optional case-insensitive title or process-name filter."},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxScreenWindows, "description": "Maximum number of windows to return. Defaults to 50."},
				},
				"additionalProperties": false,
			},
		},
		{
			Name:        "screen_get_active_window",
			Title:       "Get Active Window",
			Description: "Return metadata for the current foreground window without taking a screenshot.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
		{
			Name:        "screen_capture_window",
			Title:       "Capture Window",
			Description: "Capture one explicitly selected Windows application window on demand and return a PNG image to the MCP client. Nothing is saved to disk.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"window":   map[string]any{"type": "string", "minLength": 1, "maxLength": 500, "description": "Window id from screen_list_windows, exact title, or a unique case-insensitive title substring."},
					"maxWidth": captureProperties["maxWidth"],
				},
				"required":             []string{"window"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "screen_capture_active_window",
			Title:       "Capture Active Window",
			Description: "Capture the current foreground window on demand and return a PNG image to the MCP client. Nothing is saved to disk.",
			InputSchema: map[string]any{"type": "object", "properties": captureProperties, "additionalProperties": false},
		},
		{
			Name:        "screen_capture_desktop",
			Title:       "Capture Desktop",
			Description: "Capture the Windows virtual desktop across connected monitors on demand and return a PNG image. Nothing is saved to disk.",
			InputSchema: map[string]any{"type": "object", "properties": captureProperties, "additionalProperties": false},
		},
	}
}

func (s *Server) executeScreenTool(name string, arguments map[string]any) (map[string]any, error) {
	if err := s.requireScreenCapturePermission(); err != nil {
		return nil, err
	}
	if err := s.enforceScreenVisionToolPolicy(name); err != nil {
		return nil, err
	}
	switch name {
	case "screen_list_windows":
		var args screenListArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		windows, err := platformListScreenWindows()
		if err != nil {
			return nil, err
		}
		query := strings.ToLower(strings.TrimSpace(args.Query))
		filtered := make([]screenWindow, 0, len(windows))
		for _, window := range windows {
			if query != "" && !strings.Contains(strings.ToLower(window.Title), query) && !strings.Contains(strings.ToLower(window.ProcessName), query) {
				continue
			}
			filtered = append(filtered, window)
		}
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].Active != filtered[j].Active {
				return filtered[i].Active
			}
			return strings.ToLower(filtered[i].Title) < strings.ToLower(filtered[j].Title)
		})
		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		if limit > maxScreenWindows {
			limit = maxScreenWindows
		}
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
		return map[string]any{
			"windows":       filtered,
			"count":         len(filtered),
			"captureActive": false,
		}, nil
	case "screen_get_active_window":
		window, err := platformActiveScreenWindow()
		if err != nil {
			return nil, err
		}
		return map[string]any{"window": window, "captureActive": false}, nil
	case "screen_capture_window":
		var args screenWindowArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		windowArgument, err := s.screenVisionWindowArgument(args.Window)
		if err != nil {
			return nil, err
		}
		args.Window = windowArgument
		windows, err := platformListScreenWindows()
		if err != nil {
			return nil, err
		}
		window, err := resolveScreenWindow(windows, args.Window)
		if err != nil {
			return nil, err
		}
		if err := s.validateScreenVisionWindow(window); err != nil {
			return nil, err
		}
		frame, err := platformCaptureScreenWindow(window)
		if err != nil {
			return nil, err
		}
		return screenFrameResult(frame, args.MaxWidth, &window)
	case "screen_capture_active_window":
		var args screenCaptureArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		window, err := platformActiveScreenWindow()
		if err != nil {
			return nil, err
		}
		frame, err := platformCaptureScreenWindow(window)
		if err != nil {
			return nil, err
		}
		return screenFrameResult(frame, args.MaxWidth, &window)
	case "screen_capture_desktop":
		var args screenCaptureArgs
		if err := decodeToolArguments(arguments, &args); err != nil {
			return nil, err
		}
		frame, err := platformCaptureScreenDesktop()
		if err != nil {
			return nil, err
		}
		return screenFrameResult(frame, args.MaxWidth, nil)
	default:
		return nil, fmt.Errorf("unknown screen tool: %s", name)
	}
}

func (s *Server) requireScreenCapturePermission() error {
	if !s.screenCaptureEnabled {
		return errors.New("Screen Vision is disabled. Enable on-demand screen capture in MCP DevDesk Security settings first")
	}
	if s.permissionMode != "trusted" && s.permissionMode != "dangerous" {
		return errors.New("Screen Vision requires trusted or dangerous permission mode")
	}
	return nil
}

func resolveScreenWindow(windows []screenWindow, value string) (screenWindow, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return screenWindow{}, errors.New("window is required")
	}
	if handle, ok := parseScreenWindowID(value); ok {
		for _, window := range windows {
			if window.Handle == handle {
				return window, nil
			}
		}
		return screenWindow{}, fmt.Errorf("window %q is no longer available; call screen_list_windows again", value)
	}
	for _, window := range windows {
		if strings.EqualFold(window.Title, value) {
			return window, nil
		}
	}
	needle := strings.ToLower(value)
	matches := make([]screenWindow, 0, 4)
	for _, window := range windows {
		if strings.Contains(strings.ToLower(window.Title), needle) {
			matches = append(matches, window)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return screenWindow{}, fmt.Errorf("no visible window matches %q", value)
	}
	candidates := make([]string, 0, len(matches))
	for i, match := range matches {
		if i >= 8 {
			break
		}
		candidates = append(candidates, fmt.Sprintf("%s (%s)", match.Title, match.ID))
	}
	return screenWindow{}, fmt.Errorf("window %q is ambiguous; choose an id from screen_list_windows: %s", value, strings.Join(candidates, ", "))
}

func parseScreenWindowID(value string) (uintptr, bool) {
	value = strings.TrimSpace(value)
	base := 10
	number := value
	if strings.HasPrefix(strings.ToLower(value), "0x") {
		base = 16
		number = value[2:]
	}
	parsed, err := strconv.ParseUint(number, base, 64)
	if err != nil || parsed == 0 {
		return 0, false
	}
	return uintptr(parsed), true
}

func screenFrameResult(frame screenCaptureFrame, maxWidth int, window *screenWindow) (map[string]any, error) {
	if frame.Image == nil || frame.Image.Bounds().Dx() <= 0 || frame.Image.Bounds().Dy() <= 0 {
		return nil, errors.New("screen capture returned an empty image")
	}
	maxWidth, err := normalizeScreenMaxWidth(maxWidth)
	if err != nil {
		return nil, err
	}
	originalWidth := frame.Image.Bounds().Dx()
	originalHeight := frame.Image.Bounds().Dy()
	imageValue := frame.Image
	if originalWidth > maxWidth {
		imageValue = scaleScreenNRGBA(frame.Image, maxWidth)
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, imageValue); err != nil {
		return nil, fmt.Errorf("encode screen capture: %w", err)
	}
	if buffer.Len() > maxImageBytes {
		return nil, fmt.Errorf("screen capture is %d bytes after PNG encoding; maximum is %d bytes. Try a smaller maxWidth", buffer.Len(), maxImageBytes)
	}
	digest := sha256.Sum256(buffer.Bytes())
	result := map[string]any{
		"_mcpImageData":     base64.StdEncoding.EncodeToString(buffer.Bytes()),
		"_mcpImageMimeType": "image/png",
		"mimeType":          "image/png",
		"width":             imageValue.Bounds().Dx(),
		"height":            imageValue.Bounds().Dy(),
		"originalWidth":     originalWidth,
		"originalHeight":    originalHeight,
		"sizeBytes":         buffer.Len(),
		"sha256":            hex.EncodeToString(digest[:]),
		"captureMethod":     frame.Method,
		"bounds":            frame.Bounds,
		"capturedAt":        time.Now().UTC().Format(time.RFC3339Nano),
		"persisted":         false,
		"continuous":        false,
	}
	if window != nil {
		result["window"] = *window
	}
	return result, nil
}

func normalizeScreenMaxWidth(value int) (int, error) {
	if value == 0 {
		return defaultScreenMaxWidth, nil
	}
	if value < minScreenMaxWidth || value > maxScreenMaxWidth {
		return 0, fmt.Errorf("maxWidth must be between %d and %d", minScreenMaxWidth, maxScreenMaxWidth)
	}
	return value, nil
}

func scaleScreenNRGBA(source *image.NRGBA, width int) *image.NRGBA {
	sourceWidth := source.Bounds().Dx()
	sourceHeight := source.Bounds().Dy()
	if width <= 0 || width >= sourceWidth {
		return source
	}
	height := sourceHeight * width / sourceWidth
	if height < 1 {
		height = 1
	}
	destination := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := y * sourceHeight / height
		for x := 0; x < width; x++ {
			sx := x * sourceWidth / width
			sourceOffset := source.PixOffset(source.Bounds().Min.X+sx, source.Bounds().Min.Y+sy)
			destinationOffset := destination.PixOffset(x, y)
			copy(destination.Pix[destinationOffset:destinationOffset+4], source.Pix[sourceOffset:sourceOffset+4])
		}
	}
	return destination
}
