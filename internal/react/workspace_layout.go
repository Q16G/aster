package react

import "aster/internal/workspacefs"

// wsLayout 返回当前 agent workspace 的路径布局（react 侧路径唯一来源）。
func (a *Agent) wsLayout() workspacefs.Layout {
	root := a.workspaceRootDir
	if a.workspaceRuntime != nil {
		root = a.workspaceRuntime.RootDir()
	}
	return workspacefs.New(root, a.workspaceNamespace)
}
