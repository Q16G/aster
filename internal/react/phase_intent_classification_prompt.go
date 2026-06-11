package react

import (
	_ "embed"
)

//go:embed prompts/intent_classification_system.prompt
var intentClassificationSystemPrompt string

//go:embed prompts/intent_classification_user.prompt
var intentClassificationUserPrompt string
