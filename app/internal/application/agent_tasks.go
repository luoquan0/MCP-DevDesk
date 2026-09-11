package application

import "mcp-devdesk/internal/agentstate"

type AgentTaskList struct {
	Tasks        []agentstate.Task `json:"tasks"`
	ActiveTaskID string            `json:"activeTaskId,omitempty"`
}

func (a *App) AgentTasks() (AgentTaskList, error) {
	if active, ok, _ := a.agentTasks.Active(); ok {
		_, _ = a.agentTasks.RefreshChangedFiles(active.ID)
	}
	tasks, activeID, err := a.agentTasks.List()
	if err != nil {
		return AgentTaskList{}, err
	}
	return AgentTaskList{Tasks: tasks, ActiveTaskID: activeID}, nil
}

func (a *App) AcceptAgentTask(id string) (agentstate.Task, error) {
	return a.agentTasks.Accept(id)
}

func (a *App) RejectAgentTask(id string) (agentstate.Task, error) {
	return a.agentTasks.Reject(id)
}
