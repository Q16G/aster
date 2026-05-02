package tuicontext

type KeyAction string

const (
	KeyActionQuit         KeyAction = "quit"
	KeyActionCancel       KeyAction = "cancel"
	KeyActionNewSession   KeyAction = "new_session"
	KeyActionOpenSessions KeyAction = "open_sessions"
	KeyActionOpenAgents   KeyAction = "open_agents"
	KeyActionOpenModels   KeyAction = "open_models"
	KeyActionClearChat    KeyAction = "clear_chat"
	KeyActionCycleFocus   KeyAction = "cycle_focus"
	KeyActionEscape       KeyAction = "escape"
)

type KeybindMap struct {
	Bindings map[string]KeyAction
}

type KeybindProvider struct {
	*Provider[KeybindMap]
}

func DefaultKeybinds() KeybindMap {
	return KeybindMap{
		Bindings: map[string]KeyAction{
			"ctrl+k": KeyActionOpenAgents,
			"ctrl+n": KeyActionNewSession,
			"ctrl+o": KeyActionOpenSessions,
			"ctrl+m": KeyActionOpenModels,
			"ctrl+l": KeyActionClearChat,
			"tab":    KeyActionCycleFocus,
			"esc":    KeyActionEscape,
		},
	}
}

func NewKeybindProvider() *KeybindProvider {
	return &KeybindProvider{
		Provider: NewProvider(DefaultKeybinds()),
	}
}

func (k *KeybindProvider) Resolve(keyStr string) (KeyAction, bool) {
	km := k.Get()
	action, ok := km.Bindings[keyStr]
	return action, ok
}
