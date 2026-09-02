package application

import (
	"context"
	"fmt"
	"time"

	"mcp-devdesk/internal/model"
)

func shouldAutoStartProjectInstance(primaryWorkspace string, cfg model.Config) bool {
	return cfg.AutoStart && !samePath(primaryWorkspace, cfg.Workspace)
}

// StartAutoProjectInstances restores only independent project runtimes.
// Historical versions allowed the primary workspace to also have a managed
// instance; once that project is treated as the main project, restoring the
// duplicate would start a second MCP process for the same workspace.
func (a *App) StartAutoProjectInstances(ctx context.Context) []error {
	primaryWorkspace := a.config.Get().Workspace
	var failures []error
	for _, record := range a.instances.List() {
		_, runtime, err := a.instanceRecordAndRuntime(record.ID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		cfg := runtime.config.Get()
		if !shouldAutoStartProjectInstance(primaryWorkspace, cfg) {
			continue
		}
		instanceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, startErr := a.StartInstance(instanceCtx, record.ID)
		cancel()
		if startErr != nil {
			failures = append(failures, fmt.Errorf("start %s: %w", record.Name, startErr))
		}
	}
	return failures
}
