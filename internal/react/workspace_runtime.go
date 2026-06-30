package react

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"aster/internal/builtin_tools"
)

type localWorkspaceRuntime struct {
	sessionID string
	rootDir   string
	namespace string

	// sharedFileLocks 给 task_context.md / open_items.md 加 per-file RWMutex。
	//
	// **保护范围（实事求是）**：仅覆盖通过 WriteFileRel / ReadFileRel 走的写读——
	// 即 runtime 自身的 skeleton 初始化（EnsureSharedScaffold、ensureTaskContextSkeleton）
	// 和 commit 6 接进 runtime 的 prompt 注入读路径。
	//
	// **不覆盖**：LLM 通过 BashTool（`cat > file` / `tee` / `echo >>` 等）写 ledger
	// 的真实路径——bash 进程直接 open() syscall，绕开 WorkspaceRuntime。当前没有
	// write_file 工具强制 LLM 走 runtime，所以本锁对最高频写者无效。
	//
	// **lost-update 防线**：全部由 prompt 纪律承担（per-step OI 命名空间 + 本 step 只动
	// 自己来源条目 + 已闭环迁移由 step_replan 串行整合）。参见
	// [[feedback_no_atomic_ledger_tools]]。
	//
	// 本锁存在的实际意义：为未来引入 write_file 工具（让 LLM 写 ledger 经 runtime）
	// 时的 hook 占位——届时此锁直接生效；现状下它保护 skeleton 初始化的少量并发场景
	// （EnsureSharedScaffold 已有 os.Stat 幂等检查，所以保护面更窄）。
	sharedFileLocksMu sync.Mutex
	sharedFileLocks   map[string]*sync.RWMutex

	// stateRMWMu 串行化 workspace/state.json 的 load-mutate-save 临界区，供
	// MutateChildAgent（以及未来的同款 RMW 接口）使用。与 sharedFileLockFor 正交：
	// 后者保护 task_context.md / open_items.md 的 WriteFileRel/ReadFileRel 路径，本锁
	// 保护 LoadWorkspaceState/SaveWorkspaceState 的原子组合。
	//
	// **为什么需要**：sub_agent_tool.go 的 preRegisterChildAgent / finalizeChildAgent
	// 都做 Load→改 ChildAgents→Save；同一 think_act 回合多路并发 sub_agent 派发时
	// （A.1+A.2 解锁后），load-A → load-B → save-A → save-B 会丢更新。本锁把
	// load-mutate-save 包成单临界区，根除 lost-update。
	stateRMWMu sync.Mutex
}

var _ builtin_tools.WorkspaceRuntime = (*localWorkspaceRuntime)(nil)

func newLocalWorkspaceRuntime(sessionID string, rootDir string, namespace string) (builtin_tools.WorkspaceRuntime, error) {
	rootDir = normalizeWorkspaceRootDir(rootDir)
	if strings.TrimSpace(rootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is empty")
	}
	return &localWorkspaceRuntime{
		sessionID:       strings.TrimSpace(sessionID),
		rootDir:         rootDir,
		namespace:       builtin_tools.NormalizeWorkspaceNamespace(namespace),
		sharedFileLocks: make(map[string]*sync.RWMutex),
	}, nil
}

// sharedFileLockKeys 列出需要 per-file RWMutex 保护的共享 ledger 路径
// （相对 workspace root 的 slash 形式）。命中 → 取/建对应锁；不命中 → 返回 nil 不上锁。
//
// 当前仅保护 task_context.md / open_items.md（inline_step 并发写者；其他 shared 文件如
// planner_skills_index 由 planner 单写、step_p*-s*.md 由 step 自己一写一读，无并发）。
var sharedFileLockKeys = map[string]struct{}{
	"shared/task_context.md": {},
	"shared/open_items.md":   {},
}

// sharedFileLockFor 返回 relPath 对应的 per-file 锁；非保护路径返回 nil。
// 锁的生命周期与 localWorkspaceRuntime 实例绑定，按需懒建。
func (w *localWorkspaceRuntime) sharedFileLockFor(relPath string) *sync.RWMutex {
	if w == nil {
		return nil
	}
	key := filepath.ToSlash(strings.TrimSpace(relPath))
	if _, ok := sharedFileLockKeys[key]; !ok {
		return nil
	}
	w.sharedFileLocksMu.Lock()
	defer w.sharedFileLocksMu.Unlock()
	if w.sharedFileLocks == nil {
		w.sharedFileLocks = make(map[string]*sync.RWMutex)
	}
	if lock, ok := w.sharedFileLocks[key]; ok {
		return lock
	}
	lock := &sync.RWMutex{}
	w.sharedFileLocks[key] = lock
	return lock
}

func (w *localWorkspaceRuntime) SessionID() string {
	if w == nil {
		return ""
	}
	return w.sessionID
}

func (w *localWorkspaceRuntime) RootDir() string {
	if w == nil {
		return ""
	}
	return w.rootDir
}

func (w *localWorkspaceRuntime) Namespace() string {
	if w == nil {
		return ""
	}
	return builtin_tools.NormalizeWorkspaceNamespace(w.namespace)
}

func (w *localWorkspaceRuntime) SharedDir() string {
	if w == nil || w.rootDir == "" {
		return ""
	}
	return filepath.Join(w.rootDir, "shared")
}

// 骨架内容须与 prompt 期望的标题逐字一致：task_planner.prompt 原则 0.7、
// think_act.prompt 5.1e（task_context.md）；open_items.md 由 think_act.prompt 5.1f（step 入口）
// 与 step_replan.prompt 原则 7.1（消费/补合成）双写者共同维护。
// 标题不一致会让模型新建重复标题。
const taskContextScaffold = "# 贯穿全程关键事实\n\n## 输入事实\n\n## 执行中补充\n"

// 未闭环账本单文件三区（未解决 / 不可解局限 / 已闭环），与 main 同构：闭环项就地迁入
// `## 已闭环`，闭环状态与未解决项同处可见。账本由 AI（think_act / planning）直接维护——
// 含子 Agent 终态后由 think_act 按主视角重判归类的部分（见 think_act_system.prompt
// 子 Agent 委派段「产出归并」原则）。条目格式宽松（建议保留 OI-xxx 编号习惯与
// 来源/证据标注）。三区节标题是 prompt 与 AI 写者的共同契约，须逐字一致；无 H1（避免被改写）。
// 注入按 sharedFileLimitBytes 从尾部截断，`## 已闭环` 最先被截、`## 未解决` 始终保留。
const openItemsScaffold = "## 未解决\n\n## 不可解局限\n\n## 已闭环\n"

// EnsureSharedScaffold 在 shared 目录下为 task_context.md 与 open_items.md 预置骨架，
// 保证两文件确定性存在（内容仍由模型按各 prompt 纪律覆盖写入）。仅当文件不存在时写入，
// 已存在则原样跳过，绝不覆盖既有内容（保护 resume / 已写入场景）。
func (w *localWorkspaceRuntime) EnsureSharedScaffold() error {
	sharedDir := w.SharedDir()
	if strings.TrimSpace(sharedDir) == "" {
		return nil
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return err
	}
	scaffolds := map[string]string{
		"task_context.md": taskContextScaffold,
		"open_items.md":   openItemsScaffold,
	}
	for name, content := range scaffolds {
		absPath := filepath.Join(sharedDir, name)
		if _, err := os.Stat(absPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (w *localWorkspaceRuntime) ReadFileRel(relPath string) ([]byte, error) {
	absPath, err := w.resolveAbsPath(relPath)
	if err != nil {
		return nil, err
	}
	if lock := w.sharedFileLockFor(relPath); lock != nil {
		lock.RLock()
		defer lock.RUnlock()
	}
	return os.ReadFile(absPath)
}

func (w *localWorkspaceRuntime) WriteFileRel(relPath string, content []byte) error {
	absPath, err := w.resolveAbsPath(relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	if lock := w.sharedFileLockFor(relPath); lock != nil {
		lock.Lock()
		defer lock.Unlock()
	}
	return os.WriteFile(absPath, content, 0o644)
}

func (w *localWorkspaceRuntime) LoadWorkspaceState() (*builtin_tools.WorkspaceState, error) {
	data, err := w.ReadFileRel(filepath.ToSlash(filepath.Join("workspace", "state.json")))
	if err != nil {
		if os.IsNotExist(err) {
			return &builtin_tools.WorkspaceState{
				SessionID:          strings.TrimSpace(w.SessionID()),
				LatestStepOutcomes: make(map[string]*builtin_tools.WorkspaceStepOutcomePointer),
				ChildAgents:        make(map[string]*builtin_tools.WorkspaceChildAgentPointer),
			}, nil
		}
		return nil, err
	}
	var state builtin_tools.WorkspaceState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal workspace state: %w", err)
	}
	if state.LatestStepOutcomes == nil {
		state.LatestStepOutcomes = make(map[string]*builtin_tools.WorkspaceStepOutcomePointer)
	}
	if state.ChildAgents == nil {
		state.ChildAgents = make(map[string]*builtin_tools.WorkspaceChildAgentPointer)
	}
	if state.SessionID == "" {
		state.SessionID = strings.TrimSpace(w.SessionID())
	}
	return &state, nil
}

func (w *localWorkspaceRuntime) SaveWorkspaceState(state *builtin_tools.WorkspaceState) error {
	if state == nil {
		return fmt.Errorf("workspace state is nil")
	}
	if state.LatestStepOutcomes == nil {
		state.LatestStepOutcomes = make(map[string]*builtin_tools.WorkspaceStepOutcomePointer)
	}
	if state.ChildAgents == nil {
		state.ChildAgents = make(map[string]*builtin_tools.WorkspaceChildAgentPointer)
	}
	if strings.TrimSpace(state.SessionID) == "" {
		state.SessionID = strings.TrimSpace(w.SessionID())
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace state: %w", err)
	}
	data = append(data, '\n')
	return w.WriteFileRel(filepath.ToSlash(filepath.Join("workspace", "state.json")), data)
}

// MutateChildAgent 在 stateRMWMu 临界区内 load-mutate-save state.ChildAgents[name]，
// 根除并发 sub_agent 派发时 preRegister/finalize 的 lost-update（参见结构体注释）。
// mutate 收 prev 指针（nil = 首次插入），返回新指针；返回 nil 等价"删除该条"。
func (w *localWorkspaceRuntime) MutateChildAgent(
	name string,
	mutate func(prev *builtin_tools.WorkspaceChildAgentPointer) *builtin_tools.WorkspaceChildAgentPointer,
) error {
	if w == nil {
		return fmt.Errorf("workspace runtime is nil")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("child agent name is empty")
	}
	if mutate == nil {
		return fmt.Errorf("mutate fn is nil")
	}
	w.stateRMWMu.Lock()
	defer w.stateRMWMu.Unlock()
	state, err := w.LoadWorkspaceState()
	if err != nil {
		return err
	}
	if state == nil {
		state = &builtin_tools.WorkspaceState{
			SessionID:          strings.TrimSpace(w.SessionID()),
			LatestStepOutcomes: make(map[string]*builtin_tools.WorkspaceStepOutcomePointer),
			ChildAgents:        make(map[string]*builtin_tools.WorkspaceChildAgentPointer),
		}
	}
	if state.ChildAgents == nil {
		state.ChildAgents = make(map[string]*builtin_tools.WorkspaceChildAgentPointer)
	}
	prev := state.ChildAgents[name]
	next := mutate(prev)
	if next == nil {
		delete(state.ChildAgents, name)
	} else {
		state.ChildAgents[name] = next
	}
	return w.SaveWorkspaceState(state)
}

func (w *localWorkspaceRuntime) LoadWorkspaceReferences() ([]*builtin_tools.WorkspaceReferenceRecord, error) {
	data, err := w.ReadFileRel(filepath.ToSlash(filepath.Join("workspace", "references.jsonl")))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	out := make([]*builtin_tools.WorkspaceReferenceRecord, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record builtin_tools.WorkspaceReferenceRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("unmarshal workspace reference: %w", err)
		}
		record.RefID = strings.TrimSpace(record.RefID)
		if record.RefID == "" {
			continue
		}
		out = append(out, &record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan workspace references: %w", err)
	}
	return out, nil
}

func (w *localWorkspaceRuntime) AppendWorkspaceReferences(refs []*builtin_tools.WorkspaceReferenceRecord) error {
	if len(refs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, record := range refs {
		if record == nil || strings.TrimSpace(record.RefID) == "" {
			continue
		}
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal workspace reference: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return nil
	}
	absPath, err := w.resolveAbsPath(filepath.ToSlash(filepath.Join("workspace", "references.jsonl")))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open workspace references: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("append workspace references: %w", err)
	}
	return nil
}

func (w *localWorkspaceRuntime) LoadStepContextRecords(limit int) ([]*builtin_tools.StepContextRecord, error) {
	return builtin_tools.LoadWorkspaceStepContextRecords(w.RootDir(), limit)
}

func (w *localWorkspaceRuntime) AppendStepContextRecords(records []*builtin_tools.StepContextRecord) error {
	return builtin_tools.AppendWorkspaceStepContextRecords(w.RootDir(), records)
}

func (w *localWorkspaceRuntime) ArtifactWritePath(relPath string) (artifactPath string, absPath string, err error) {
	return builtin_tools.WorkspaceArtifactWritePath(w.RootDir(), w.Namespace(), relPath)
}

func (w *localWorkspaceRuntime) resolveAbsPath(relPath string) (string, error) {
	rootDir := strings.TrimSpace(w.RootDir())
	if rootDir == "" {
		return "", fmt.Errorf("workspace root dir is empty")
	}
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if relPath == "" {
		return "", fmt.Errorf("workspace relative path is empty")
	}
	if strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "\\") {
		return "", fmt.Errorf("workspace relative path must be relative")
	}
	localPath := filepath.Clean(filepath.FromSlash(relPath))
	if localPath == "." || localPath == "" {
		return "", fmt.Errorf("workspace relative path is empty")
	}
	if filepath.IsAbs(localPath) || localPath == ".." || strings.HasPrefix(localPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace relative path escapes root")
	}
	absPath, err := filepath.Abs(filepath.Join(rootDir, localPath))
	if err != nil {
		return "", fmt.Errorf("resolve workspace file path: %w", err)
	}
	workspaceAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root dir: %w", err)
	}
	relToRoot, err := filepath.Rel(workspaceAbs, absPath)
	if err != nil {
		return "", fmt.Errorf("resolve workspace file rel: %w", err)
	}
	relToRoot = filepath.Clean(relToRoot)
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("workspace file path escapes root")
	}
	return absPath, nil
}
