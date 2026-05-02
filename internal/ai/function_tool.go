package ai

// FunctionTool 函数工具定义
type FunctionTool struct {
	Index    *int            `json:"index,omitempty"` // 流式响应中的索引
	Id       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function *FunctionDetail `json:"function,omitempty"`
}

// FunctionDetail 函数详情
type FunctionDetail struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Arguments   any    `json:"arguments,omitempty"`
}

// Param 参数定义
type Param struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
	Enum        []any  `json:"enum,omitempty"`
}

// ObjectParam 对象参数定义
type ObjectParam struct {
	Type       string   `json:"type,omitempty"`
	Properties []Param  `json:"properties,omitempty"`
	Required   []string `json:"required,omitempty"`
}

// NewFunctionCall 创建函数调用定义
func NewFunctionCall(name string, description string, params *ObjectParam) *FunctionDetail {
	return &FunctionDetail{
		Name:        name,
		Description: description,
		Parameters:  params,
	}
}

// NewFunctionTool 创建函数工具
func NewFunctionTool(name, description string, params *ObjectParam) *FunctionTool {
	return &FunctionTool{
		Type: "function",
		Function: &FunctionDetail{
			Name:        name,
			Description: description,
			Parameters:  params,
		},
	}
}
