package semgrep_rules

import "embed"

//go:embed java/**/*.yaml go/**/*.yaml python/**/*.yaml javascript/**/*.yaml php/**/*.yaml c-cpp/**/*.yaml
var EmbeddedRules embed.FS
