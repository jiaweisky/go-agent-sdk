package core

import "github.com/user/go-agent-sdk/schema"

type (
	AgentID    = schema.AgentID
	AgentColor = schema.AgentColor
	AgentInfo  = schema.AgentInfo
)

// AgentDefaultID is the agent a session uses when it names none.
const AgentDefaultID = schema.AgentID("build")

type AgentSelection struct {
	ID   schema.AgentID
	Info *schema.AgentInfo
}

type agentData struct {
	agents    map[schema.AgentID]*schema.AgentInfo
	order     []schema.AgentID
	defaultID *schema.AgentID
}

// AgentDraft is the mutable view passed to registered transforms.
type AgentDraft struct {
	List    func() []schema.AgentInfo
	Get     func(schema.AgentID) *schema.AgentInfo
	Default func(*schema.AgentID)
	Update  func(schema.AgentID, func(*schema.AgentInfo))
	Remove  func(schema.AgentID)
}

// Agent is the agent registry. Agents are held in an ordered map so listing and
// fallback selection follow registration order.
type Agent struct {
	state *State[*agentData, AgentDraft]
}

func NewAgent(batch ...*StateBatch) *Agent {
	agent := &Agent{}
	options := StateOptions[*agentData, AgentDraft]{
		Initial: func() *agentData {
			return &agentData{agents: map[schema.AgentID]*schema.AgentInfo{}}
		},
		Draft: func(data *agentData) AgentDraft {
			return AgentDraft{
				List: func() []schema.AgentInfo {
					list := make([]schema.AgentInfo, 0, len(data.order))
					for _, id := range data.order {
						list = append(list, *data.agents[id])
					}
					return list
				},
				Get: func(id schema.AgentID) *schema.AgentInfo {
					return data.agents[id]
				},
				Default: func(id *schema.AgentID) {
					data.defaultID = id
				},
				Update: func(id schema.AgentID, fn func(*schema.AgentInfo)) {
					current, ok := data.agents[id]
					if !ok {
						empty := schema.AgentInfoEmpty(id)
						current = &empty
						data.agents[id] = current
						data.order = append(data.order, id)
					}
					fn(current)
					current.ID = id
				},
				Remove: func(id schema.AgentID) {
					if _, ok := data.agents[id]; !ok {
						return
					}
					delete(data.agents, id)
					for i, item := range data.order {
						if item == id {
							data.order = append(data.order[:i], data.order[i+1:]...)
							break
						}
					}
				},
			}
		},
	}
	if len(batch) > 0 {
		options.Batch = batch[0]
	}
	agent.state = NewState(options)
	return agent
}

func (a *Agent) Transform(update func(AgentDraft) error) (func() error, error) {
	return a.state.Transform(update)
}

func (a *Agent) Reload() error {
	return a.state.Reload()
}

func (a *Agent) Get(id schema.AgentID) *schema.AgentInfo {
	return a.state.Get().agents[id]
}

func (a *Agent) Default() *schema.AgentInfo {
	return a.selectedDefault()
}

func (a *Agent) Resolve(id *string) *schema.AgentInfo {
	if id != nil {
		return a.state.Get().agents[schema.AgentID(*id)]
	}
	return a.selectedDefault()
}

func (a *Agent) Select(id *string) AgentSelection {
	if id != nil {
		selected := schema.AgentID(*id)
		return AgentSelection{ID: selected, Info: a.state.Get().agents[selected]}
	}
	info := a.selectedDefault()
	selected := AgentDefaultID
	if info != nil {
		selected = info.ID
	}
	return AgentSelection{ID: selected, Info: info}
}

func (a *Agent) All() []schema.AgentInfo {
	data := a.state.Get()
	list := make([]schema.AgentInfo, 0, len(data.order))
	for _, id := range data.order {
		list = append(list, *data.agents[id])
	}
	return list
}

func (a *Agent) selectedDefault() *schema.AgentInfo {
	data := a.state.Get()
	if data.defaultID != nil {
		if configured := agentSelectable(data.agents[*data.defaultID]); configured != nil {
			return configured
		}
	}
	if build := agentSelectable(data.agents[AgentDefaultID]); build != nil {
		return build
	}
	for _, id := range data.order {
		if fallback := agentSelectable(data.agents[id]); fallback != nil {
			return fallback
		}
	}
	return nil
}

func agentSelectable(agent *schema.AgentInfo) *schema.AgentInfo {
	if agent != nil && agent.Mode != schema.AgentModeSubagent && !agent.Hidden {
		return agent
	}
	return nil
}
