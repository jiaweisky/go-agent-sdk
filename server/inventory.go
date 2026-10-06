package server

import (
	"net/http"

	"github.com/jiaweisky/go-agent-sdk/schema"
)

func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	all := s.client.Agents().All()
	out := make([]agentInfoDTO, 0, len(all))
	for _, a := range all {
		if a.Hidden || !s.allowed(string(a.ID)) {
			continue
		}
		dto := agentInfoDTO{ID: string(a.ID), Mode: a.Mode, Hidden: a.Hidden, Backend: a.Backend}
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
		writeSDKError(w, err)
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
	if s.client.Agents().Get(schema.AgentID(agentID)) == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown agent: "+agentID)
		return
	}
	if !s.allowed(agentID) {
		writeError(w, http.StatusForbidden, "forbidden", "agent not allowed: "+agentID)
		return
	}
	tools, err := s.client.AgentTools(agentID, s.inbound)
	if err != nil {
		writeSDKError(w, err)
		return
	}
	out := make([]toolInfoDTO, 0, len(tools))
	for _, tool := range tools {
		out = append(out, toolInfoDTO{Name: tool.Name, Description: tool.Description})
	}
	writeData(w, http.StatusOK, out)
}
