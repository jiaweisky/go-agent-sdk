package server

import "net/http"

// modelInfoDTO is one model's discoverable metadata for GET /api/model.
type modelInfoDTO struct {
	ID           string   `json:"id"`
	Protocol     string   `json:"protocol"`
	Input        []string `json:"input,omitempty"`
	Sources      []string `json:"sources,omitempty"`
	ContextLimit int      `json:"contextLimit,omitempty"`
	OutputLimit  int      `json:"outputLimit,omitempty"`
	Default      bool     `json:"default,omitempty"`
}

func (s *Server) handleModelList(w http.ResponseWriter, r *http.Request) {
	out := make([]modelInfoDTO, 0, len(s.opts.Models))
	for _, model := range s.opts.Models {
		dto := modelInfoDTO{
			ID:           string(model.ID),
			Protocol:     string(model.API.Protocol),
			Input:        model.Capabilities.Input,
			ContextLimit: model.Limit.Context,
			OutputLimit:  model.Limit.Output,
			Default:      s.opts.DefaultModel != "" && string(model.ID) == s.opts.DefaultModel,
		}
		if model.Capabilities.Media != nil {
			dto.Sources = model.Capabilities.Media.Sources
		}
		out = append(out, dto)
	}
	writeData(w, http.StatusOK, out)
}

func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	all := s.client.Agents().All()
	def := s.defaultAgent()
	out := make([]agentInfoDTO, 0, len(all))
	for _, a := range all {
		if a.Hidden || !s.allowed(string(a.ID)) {
			continue
		}
		dto := agentInfoDTO{
			ID: string(a.ID), Mode: a.Mode, Hidden: a.Hidden, Backend: a.Backend,
			Default: def != "" && string(a.ID) == def,
		}
		if a.Description != nil {
			dto.Description = *a.Description
		}
		if a.Model != nil {
			dto.Model = a.Model
		}
		out = append(out, dto)
	}
	writeData(w, http.StatusOK, out)
}

func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	skills, err := s.client.Skills().List()
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	out := make([]skillInfoDTO, 0, len(skills))
	for _, skill := range skills {
		dto := skillInfoDTO{Name: skill.Name}
		if skill.Description != nil {
			dto.Description = *skill.Description
		}
		out = append(out, dto)
	}
	writeData(w, http.StatusOK, out)
}

func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "agent query parameter is required")
		return
	}
	if !s.requireAgent(w, agentID) {
		return
	}
	tools, err := s.client.AgentTools(agentID, s.inbound)
	if err != nil {
		s.writeSDKError(w, err)
		return
	}
	out := make([]toolInfoDTO, 0, len(tools))
	for _, tool := range tools {
		out = append(out, toolInfoDTO{Name: tool.Name, Description: tool.Description})
	}
	writeData(w, http.StatusOK, out)
}
