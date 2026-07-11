package react

import (
	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

// WorkspaceRuntime is the stable workspace contract consumed by runtime code.
//
// It intentionally only carries workspace lifecycle and persistence concerns:
// session identity, namespace identity, workspace state/reference persistence,
// step-context lineage persistence, artifact path resolution, and relative file IO.
type WorkspaceRuntime interface {
	SessionID() string
	RootDir() string
	Namespace() string

	// Layout 返回本 workspace 的路径布局（路径唯一来源）。
	Layout() workspacefs.Layout
	// Store 返回底层存储（Go 侧 workspace IO 的唯一入口）。
	Store() workspacefs.Store

	// SharedDir returns the absolute path to the cross-step shared workspace directory.
	// All non-step-specific and non-plan-specific data (tool outputs, evidence,
	// intermediate results, etc.) should be written here so that subsequent steps
	// can access them directly.
	SharedDir() string

	ReadFileRel(relPath string) ([]byte, error)
	WriteFileRel(relPath string, content []byte) error

	LoadWorkspaceState() (*builtin_tools.WorkspaceState, error)
	SaveWorkspaceState(state *builtin_tools.WorkspaceState) error

	LoadWorkspaceReferences() ([]*builtin_tools.WorkspaceReferenceRecord, error)
	AppendWorkspaceReferences(refs []*builtin_tools.WorkspaceReferenceRecord) error

	LoadStepContextRecords(limit int) ([]*builtin_tools.StepContextRecord, error)
	AppendStepContextRecords(records []*builtin_tools.StepContextRecord) error

	ArtifactWritePath(relPath string) (artifactPath string, absPath string, err error)
}
