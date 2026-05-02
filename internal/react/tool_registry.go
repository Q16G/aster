package react

import (
	"fmt"
	"strings"
	"sync"

	"aster/internal/builtin_tools"
)

// ToolFactory creates a Tool instance. The ToolContext parameter is available
// for tools that need runtime state access (e.g. bash); stateless tools may ignore it.
type ToolFactory func(ctx builtin_tools.ToolContext) Tool

// ToolRegistry is a name-to-factory registry that lets Agent Definitions declare
// tools by name and have them resolved at build time.
type ToolRegistry struct {
	mu        sync.RWMutex
	factories map[string]ToolFactory
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		factories: make(map[string]ToolFactory),
	}
}

// Register adds a tool factory under the given name.
func (r *ToolRegistry) Register(name string, factory ToolFactory) {
	if r == nil || factory == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	r.mu.Lock()
	r.factories[name] = factory
	r.mu.Unlock()
}

// Resolve creates a Tool instance by name. Returns an error if the name is not registered.
func (r *ToolRegistry) Resolve(name string, ctx builtin_tools.ToolContext) (Tool, error) {
	if r == nil {
		return nil, fmt.Errorf("tool registry is nil")
	}
	name = strings.TrimSpace(name)
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not registered", name)
	}
	tool := factory(ctx)
	if tool == nil {
		return nil, fmt.Errorf("tool factory for %q returned nil", name)
	}
	return tool, nil
}

// ResolveAll creates Tool instances for each name. Returns an error if any name is missing.
func (r *ToolRegistry) ResolveAll(names []string, ctx builtin_tools.ToolContext) ([]Tool, error) {
	tools := make([]Tool, 0, len(names))
	for _, name := range names {
		tool, err := r.Resolve(name, ctx)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// Has returns true if the name is registered.
func (r *ToolRegistry) Has(name string) bool {
	if r == nil {
		return false
	}
	name = strings.TrimSpace(name)
	r.mu.RLock()
	_, ok := r.factories[name]
	r.mu.RUnlock()
	return ok
}

// Names returns all registered tool names.
func (r *ToolRegistry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// NewDefaultToolRegistry creates a registry pre-populated with the builtin domain tools
// (list_files, read_file, rg). These are NOT platform-level tools — they are common
// utilities that many agents use, registered here for convenience so Agent Definitions
// can reference them by name.
func NewDefaultToolRegistry() *ToolRegistry {
	r := NewToolRegistry()
	r.Register(builtin_tools.ListFilesToolName, func(_ builtin_tools.ToolContext) Tool {
		return builtin_tools.NewListFilesTool()
	})
	r.Register(builtin_tools.ReadFileToolName, func(_ builtin_tools.ToolContext) Tool {
		return builtin_tools.NewReadFileTool()
	})
	r.Register(builtin_tools.RgToolName, func(_ builtin_tools.ToolContext) Tool {
		return builtin_tools.NewRgTool()
	})
	return r
}
