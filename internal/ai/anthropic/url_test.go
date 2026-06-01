package anthropic

import "testing"

func TestResolveMessagesURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"already has messages", "https://api.anthropic.com/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"minimax cn base", "https://api.minimaxi.com/anthropic/v1", "https://api.minimaxi.com/anthropic/v1/messages"},
		{"trailing slash", "https://api.minimaxi.com/anthropic/v1/", "https://api.minimaxi.com/anthropic/v1/messages"},
		{"bare host no scheme path", "api.example.com/anthropic/v1", "api.example.com/anthropic/v1/messages"},
		{"whitespace trimmed", "  https://api.minimaxi.com/anthropic/v1  ", "https://api.minimaxi.com/anthropic/v1/messages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveMessagesURL(tc.in); got != tc.want {
				t.Fatalf("ResolveMessagesURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
