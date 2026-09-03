package mcpcore

import (
	"sort"
	"strings"

	"mcp-devdesk/internal/model"
)

// ListScreenWindows returns captureable top-level app-window metadata, including
// minimized apps. It never captures pixels and is used by the local DevDesk manager
// so the user can explicitly choose a target for specified-window Screen Vision mode.
func ListScreenWindows() ([]model.ScreenWindowInfo, error) {
	windows, err := platformListScreenWindowsForVision()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(windows, func(i, j int) bool {
		if windows[i].Active != windows[j].Active {
			return windows[i].Active
		}
		if windows[i].Minimized != windows[j].Minimized {
			return !windows[i].Minimized
		}
		return strings.ToLower(windows[i].Title) < strings.ToLower(windows[j].Title)
	})
	result := make([]model.ScreenWindowInfo, 0, len(windows))
	for _, window := range windows {
		result = append(result, model.ScreenWindowInfo{
			ID:          window.ID,
			Title:       window.Title,
			ProcessID:   window.ProcessID,
			ProcessName: window.ProcessName,
			Bounds: model.ScreenRect{
				X:      window.Bounds.X,
				Y:      window.Bounds.Y,
				Width:  window.Bounds.Width,
				Height: window.Bounds.Height,
			},
			Active:    window.Active,
			Minimized: window.Minimized,
		})
	}
	return result, nil
}
