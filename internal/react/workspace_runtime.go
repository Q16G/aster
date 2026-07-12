package react

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

// localWorkspaceRuntime 是 WorkspaceRuntime 的本地文件系统实现，收口为
// workspacefs.Layout（路径唯一来源）+ workspacefs.Store（IO 唯一入口）。
//
// 并发语义：Store 对全部 key per-key RWMutex（超集覆盖旧 sharedFileLockKeys
// 白名单——原本仅 task_context.md / open_items.md / prompt_context/ 九个文件，
// 现在任意经 runtime 相对路径 API 的读写都互斥）。
//
// **lost-update 防线**：仍全部由 prompt 纪律承担（per-step OI 命名空间 + 本 step
// 只动自己来源条目 + 已闭环迁移由 step_replan 串行整合）。参见
// [[feedback_no_atomic_ledger_tools]]。工具直接写 ledger 真实路径时依旧绕开
// runtime 锁，这与旧白名单时代一致。
type localWorkspaceRuntime struct {
	sessionID string
	rootDir   string
	namespace string
	layout    workspacefs.Layout
	store     workspacefs.Store
	// stateMu 串行化 state.json 的 read-modify-write 序列（MutateWorkspaceState）。
	// state.json 是单文件多区（ChildAgents / ActiveSkillNames / LatestStepOutcomes...），
	// 多 goroutine（scheduler drain、inline peer 的 skill/子 Agent 写、bootstrap）各自
	// 「读全量→改一区→写全量」交错会跨区丢更新（WriteAtomic 只防半截读、不防丢更新）。
	// 全部写者统一走 MutateWorkspaceState 持此锁，消除 RMW 竞态（runtime mutex 兜底）。
	stateMu sync.Mutex
}

var _ WorkspaceRuntime = (*localWorkspaceRuntime)(nil)

func newLocalWorkspaceRuntime(sessionID string, rootDir string, namespace string) (WorkspaceRuntime, error) {
	rootDir = normalizeWorkspaceRootDir(rootDir)
	if strings.TrimSpace(rootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is empty")
	}
	store, err := workspacefs.NewLocalStore(rootDir)
	if err != nil {
		return nil, err
	}
	return &localWorkspaceRuntime{
		sessionID: strings.TrimSpace(sessionID),
		rootDir:   rootDir,
		namespace: builtin_tools.NormalizeWorkspaceNamespace(namespace),
		layout:    workspacefs.New(rootDir, namespace),
		store:     store,
	}, nil
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

func (w *localWorkspaceRuntime) Layout() workspacefs.Layout {
	if w == nil {
		return workspacefs.Layout{}
	}
	return w.layout
}

func (w *localWorkspaceRuntime) Store() workspacefs.Store {
	if w == nil {
		return nil
	}
	return w.store
}

func (w *localWorkspaceRuntime) SharedDir() string {
	if w == nil || w.rootDir == "" {
		return ""
	}
	return w.layout.SharedDir()
}

// 骨架内容须与 prompt 期望的标题逐字一致：task_planner.prompt 原则 0.7、
// think_act.prompt 5.1e（task_context.md）；open_items.md 由 think_act.prompt 5.1f（step 入口）
// 与 step_replan.prompt 原则 7.1（消费/补合成）双写者共同维护。
// 标题不一致会让模型新建重复标题。
// `## 历史演进` 为末节：被新值取代的旧值由 step_replan（唯一串行写者）原文迁入，
// 供前提变化判定回溯；注入按 promptPreviewTokens 保头截尾，历史区最先被截、现值区
// （`## 输入事实` / `## 执行中补充`）始终保留——与 open_items 的节序截断契约同构。
const taskContextScaffold = "# 贯穿全程关键事实\n\n## 输入事实\n\n## 执行中补充\n\n## 历史演进\n"

// 未闭环账本单文件三区（未解决 / 不可解局限 / 已闭环），与 main 同构：闭环项就地迁入
// `## 已闭环`，闭环状态与未解决项同处可见。账本由 AI（think_act / planning）直接维护——
// 含子 Agent 终态后由 think_act 按主视角重判归类的部分（见 think_act_system.prompt
// 子 Agent 委派段「产出归并」原则）。条目格式宽松（建议保留 OI-xxx 编号习惯与
// 来源/证据标注）。三区节标题是 prompt 与 AI 写者的共同契约，须逐字一致；无 H1（避免被改写）。
// 注入按 promptPreviewTokens 动态上限从尾部截断，`## 已闭环` 最先被截、`## 未解决` 始终保留。
const openItemsScaffold = "## 未解决\n\n## 不可解局限\n\n## 已闭环\n"

// EnsureSharedScaffold 在 shared 目录下为 task_context.md 与 open_items.md 预置骨架，
// 保证两文件确定性存在（内容仍由模型按各 prompt 纪律覆盖写入）。仅当文件不存在时写入，
// 已存在则原样跳过，绝不覆盖既有内容（保护 resume / 已写入场景）。
func (w *localWorkspaceRuntime) EnsureSharedScaffold() error {
	if strings.TrimSpace(w.SharedDir()) == "" {
		return nil
	}
	if err := w.store.EnsureDir(w.layout.SharedDirRel()); err != nil {
		return err
	}
	scaffolds := map[string]string{
		w.layout.TaskContextRel(): taskContextScaffold,
		w.layout.OpenItemsRel():   openItemsScaffold,
	}
	for rel, content := range scaffolds {
		if _, err := w.store.Stat(rel); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := w.store.Write(rel, []byte(content)); err != nil {
			return err
		}
	}
	return nil
}

func (w *localWorkspaceRuntime) ReadFileRel(relPath string) ([]byte, error) {
	return w.store.Read(relPath)
}

func (w *localWorkspaceRuntime) WriteFileRel(relPath string, content []byte) error {
	return w.store.Write(relPath, content)
}

func (w *localWorkspaceRuntime) LoadWorkspaceState() (*builtin_tools.WorkspaceState, error) {
	data, err := w.store.Read(w.layout.StateJSONRel())
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

// SaveWorkspaceState 原子写 state.json（temp+rename，经 Store.WriteAtomic）：
// 消除并发读者读到半截 JSON 的窗口；文件内容字节与旧普通写完全一致。
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
	return w.store.WriteAtomic(w.layout.StateJSONRel(), data)
}

// MutateWorkspaceState 持 stateMu 串行执行「Load → mutate → Save」，是 state.json 全部
// 写者的唯一 RMW 入口（消除跨区丢更新的竞态）。mutate 内**不得**再调 MutateWorkspaceState
// 或其它持 stateMu 的路径（非可重入）。读者（纯 LoadWorkspaceState）无需持锁——WriteAtomic
// 保证读到的是完整快照。
func (w *localWorkspaceRuntime) MutateWorkspaceState(mutate func(*builtin_tools.WorkspaceState) error) error {
	if w == nil {
		return fmt.Errorf("workspace runtime is nil")
	}
	if mutate == nil {
		return nil
	}
	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	state, err := w.LoadWorkspaceState()
	if err != nil {
		return err
	}
	if err := mutate(state); err != nil {
		return err
	}
	return w.SaveWorkspaceState(state)
}

func (w *localWorkspaceRuntime) LoadWorkspaceReferences() ([]*builtin_tools.WorkspaceReferenceRecord, error) {
	data, err := w.store.Read(w.layout.ReferencesRel())
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
	if err := w.store.Append(w.layout.ReferencesRel(), buf.Bytes()); err != nil {
		return fmt.Errorf("append workspace references: %w", err)
	}
	return nil
}

func (w *localWorkspaceRuntime) LoadStepContextRecords(limit int) ([]*builtin_tools.StepContextRecord, error) {
	return LoadWorkspaceStepContextRecords(w.RootDir(), limit)
}

func (w *localWorkspaceRuntime) AppendStepContextRecords(records []*builtin_tools.StepContextRecord) error {
	return AppendWorkspaceStepContextRecords(w.RootDir(), records)
}

func (w *localWorkspaceRuntime) ArtifactWritePath(relPath string) (artifactPath string, absPath string, err error) {
	return builtin_tools.WorkspaceArtifactWritePath(w.RootDir(), w.Namespace(), relPath)
}
