package ai

// ClientFactory 用于按需创建不同 model 的 ChatClient
// 主要用于 skill 指定 model 时动态切换
type ClientFactory interface {
	// CreateClient 根据 model ID 创建对应的 ChatClient
	// 如果 modelID 为空，返回默认 client
	CreateClient(modelID string) ChatClient

	// DefaultClient 返回默认的 ChatClient
	DefaultClient() ChatClient
}

// SimpleClientFactory 简单实现，使用回调函数创建 client
type SimpleClientFactory struct {
	defaultClient ChatClient
	createFunc    func(modelID string) ChatClient
}

// NewSimpleClientFactory 创建简单工厂
// createFunc 可以为 nil，此时 CreateClient 总是返回 defaultClient
func NewSimpleClientFactory(defaultClient ChatClient, createFunc func(modelID string) ChatClient) *SimpleClientFactory {
	return &SimpleClientFactory{
		defaultClient: defaultClient,
		createFunc:    createFunc,
	}
}

func (f *SimpleClientFactory) CreateClient(modelID string) ChatClient {
	if f.createFunc == nil {
		return f.defaultClient
	}
	client := f.createFunc(modelID)
	if client == nil {
		return f.defaultClient
	}
	return client
}

func (f *SimpleClientFactory) DefaultClient() ChatClient {
	if f.createFunc != nil {
		if client := f.createFunc(""); client != nil {
			return client
		}
	}
	return f.defaultClient
}
