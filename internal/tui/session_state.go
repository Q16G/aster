package tui

import "encoding/json"

type SessionMeta struct {
	ActiveSkillNames []string `json:"active_skill_names,omitempty"`
	ActiveMCPServers []string `json:"active_mcp_servers,omitempty"`
	Theme            string   `json:"theme,omitempty"`
}

func parseSessionMeta(raw string) SessionMeta {
	var m SessionMeta
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &m)
	}
	return m
}

func (m SessionMeta) String() string {
	data, _ := json.Marshal(m)
	return string(data)
}

func stringsContains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func stringsRemove(list []string, s string) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item != s {
			out = append(out, item)
		}
	}
	return out
}
