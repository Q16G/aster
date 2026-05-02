package memory

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed prompts/trigger.prompt
var triggerPrompt string

var triggerPromptTemplate = template.Must(template.New("trigger").Parse(triggerPrompt))

// RenderTriggerPrompt 使用 prompts/trigger.prompt 渲染长期记忆三元组萃取提示词
// - extraContext: 用于补充更高层次的上下文（任务/Agent/阶段等）
// - items: 待萃取的 TimelineItem 列表（优先使用）
// - item: 当 items 为空时使用的单条文本内容（兜底）
func RenderTriggerPrompt(extraContext string, items []*TimelineItem, item string) (string, error) {
	extraContext = strings.TrimSpace(extraContext)
	item = strings.TrimSpace(item)

	buf := bytes.NewBuffer(nil)
	if err := triggerPromptTemplate.Execute(buf, map[string]any{
		"EXTRA_CONTEXT":  extraContext,
		"COMPRESS_ITEMS": items,
		"COMPRESS_ITEM":  item,
		"NONCE":          generateRandom(8),
	}); err != nil {
		return "", fmt.Errorf("render trigger prompt failed: %w", err)
	}
	return buf.String(), nil
}
