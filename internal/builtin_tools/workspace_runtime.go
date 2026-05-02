package builtin_tools

// WorkspaceRuntime is the stable workspace contract consumed by runtime code.
//
// It intentionally only carries workspace lifecycle and persistence concerns:
// session identity, namespace identity, workspace state/reference persistence,
// step-context lineage persistence, artifact path resolution, and relative file IO.
type WorkspaceRuntime interface {
	SessionID() string
	RootDir() string
	Namespace() string

	// SharedDir returns the absolute path to the cross-step shared workspace directory.
	// All non-step-specific and non-plan-specific data (tool outputs, evidence,
	// intermediate results, etc.) should be written here so that subsequent steps
	// can access them directly.
	SharedDir() string

	ReadFileRel(relPath string) ([]byte, error)
	WriteFileRel(relPath string, content []byte) error

	LoadWorkspaceState() (*WorkspaceState, error)
	SaveWorkspaceState(state *WorkspaceState) error

	LoadWorkspaceReferences() ([]*WorkspaceReferenceRecord, error)
	AppendWorkspaceReferences(refs []*WorkspaceReferenceRecord) error

	LoadStepContextRecords(limit int) ([]*StepContextRecord, error)
	AppendStepContextRecords(records []*StepContextRecord) error

	ArtifactWritePath(relPath string) (artifactPath string, absPath string, err error)
}
