package semgrep_rules

import "embed"

//go:embed all:java all:go all:python all:javascript all:php all:c-cpp
var EmbeddedRules embed.FS
