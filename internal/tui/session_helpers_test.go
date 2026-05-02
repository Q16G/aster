package tui

import "testing"

func TestSessionMatchesQuery(t *testing.T) {
	session := &SessionRecord{
		ID:           "12345678-aaaa-bbbb-cccc-1234567890ab",
		Title:        "Audit ROOT2 instance",
		AgentName:    "code-audit",
		ProviderName: "openai",
		ModelID:      "gpt-4o",
		LastMessage:  "scan login flow",
	}

	for _, query := range []string{"root2", "audit", "code-audit", "gpt-4o", "login flow"} {
		if !sessionMatchesQuery(session, query) {
			t.Fatalf("expected session to match query %q", query)
		}
	}
	if sessionMatchesQuery(session, "not-found") {
		t.Fatalf("did not expect query to match")
	}
}
