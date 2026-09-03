package mcpcore

import (
	"sort"
	"strings"

	"mcp-devdesk/internal/model"
)

// ListScreenWindows returns only visible top-level window metadata. It never
// captures pixels and is used by the local DevDesk manager so the user can
// explicitly choose a target for specified-window Screen Vision mode.
func ListScreenWindows() ([]model.ScreenWindowInfo, error) {
	windows, err := platformListScreenWindows()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(windows, func(i, j int) bool {
		if windows[i].Active != windows[j].Active {
			return windows[i].Active
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
			Active: window.Active,
		})
	}
	return result, nil
}
