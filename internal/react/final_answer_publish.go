package react

import "strings"

// PublishedOutputContract 描述子 agent 对父层机器发布的 payload contract。
//
// - Schema/Example 用于注入到 final_answer prompt，帮助模型在 final_answer 阶段生成 published_output。
// - Validate 用于 runtime 在 final_answer 解析阶段做严格校验（可选）。
type PublishedOutputContract struct {
	// Name 可选：contract 名称（用于提示文案）。
	Name string

	// Schema 可选：published_output 的 JSON schema（推荐提供）。
	Schema string

	// Example 可选：最小示例（推荐提供）。
	Example string

	// Validate 可选：对 published_output 做结构校验的函数。输入为 published_output 的 JSON 文本。
	Validate func(string) error
}

// FinalAnswerPublishConfig 控制 final_answer 是否需要承载并发布 published_output。
// Strict=true 时：
// - published_output 必须存在且非空
// - final_answer 结构化解析失败时禁止纯文本成功 fallback
// - 若提供 Validate，则必须通过校验
type FinalAnswerPublishConfig struct {
	Contract *PublishedOutputContract
	Strict   bool
}

func normalizeFinalAnswerPublishConfig(cfg *FinalAnswerPublishConfig) *FinalAnswerPublishConfig {
	if cfg == nil || cfg.Contract == nil {
		return nil
	}
	contract := *cfg.Contract
	contract.Name = strings.TrimSpace(contract.Name)
	contract.Schema = strings.TrimSpace(contract.Schema)
	contract.Example = strings.TrimSpace(contract.Example)

	normalized := &FinalAnswerPublishConfig{
		Contract: &contract,
		Strict:   cfg.Strict,
	}
	return normalized
}

func (a *Agent) currentFinalAnswerPublishConfig() *FinalAnswerPublishConfig {
	if a == nil {
		return nil
	}
	return a.currentFinalAnswerPublish
}

func (a *Agent) SetFinalAnswerPublishConfig(cfg *FinalAnswerPublishConfig) {
	if a == nil {
		return
	}
	a.currentFinalAnswerPublish = normalizeFinalAnswerPublishConfig(cfg)
}

func (a *Agent) requiresPublishedOutput() bool {
	cfg := a.currentFinalAnswerPublishConfig()
	return cfg != nil && cfg.Strict && cfg.Contract != nil
}
