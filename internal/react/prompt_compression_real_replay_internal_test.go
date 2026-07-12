package react

// 真实会话回放压缩演示测试。
//
// 用 ~/.aster/sessions 里真实的工具调用转录（tool_start/tool_end）还原成
// []*ai.MsgInfo，在一个「上下文快要爆炸」的小窗口下驱动相位内转录压缩
// AIStepHistoryCompactor，把「原文 → 压缩后」逐轮打印出来。两条路径各一个子测试：
//
//	Layer1_就地缩短：老 tool_result 就地截短，消息结构不变（无 AI）
//	Layer2_AI折叠  ：整批老工具轮折叠成一条摘要（AI 摘要，多轮反复触发）
//
// 运行（默认自动挑最大的会话；也可用 ASTER_REPLAY_SESSION 指定会话 id 或 parts.jsonl 路径）：
//
//	go test ./internal/react/ -run TestRealSessionMultiRoundCompaction -v
//
// 不联网：Layer 2 摘要走确定性 fake summarizer（echo 真实结论摘要），可离线复现。

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"aster/internal/ai"
)

// —— 回放 fake client：窗口可配（制造溢出），Layer 2 摘要确定性且引用真实内容 ——

type replayCompactionClient struct {
	inputLimit  int
	outputLimit int
	modelName   string
	chatExCalls int
}

func (c *replayCompactionClient) ModelContextInfo() ai.ModelContextInfo {
	info := ai.ModelContextInfo{
		ModelName:        strings.TrimSpace(c.modelName),
		InputTokenLimit:  c.inputLimit,
		OutputTokenLimit: c.outputLimit,
	}
	if info.ModelName == "" {
		info.ModelName = "replay-model"
	}
	return info.Normalize()
}

func (c *replayCompactionClient) Chat(context.Context, *ai.MsgInfo, ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (c *replayCompactionClient) ChatText(context.Context, string, ...*ai.FunctionTool) (string, error) {
	return "", nil
}

// ChatEx 模拟摘要模型：从被折叠的 excerpt 里抓最长的一段文本，取前 200 runes 作为
// 「关键结论」，拼成确定性摘要。这样打印出的「压缩后」文本可读且确实源自真实工具输出。
func (c *replayCompactionClient) ChatEx(_ context.Context, infos []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.chatExCalls++
	gist := longestTextGist(infos, 200)
	summary := fmt.Sprintf("【折叠摘要#%d】已将若干轮工具调用压缩为要点；代表性结论：%s", c.chatExCalls, gist)
	return []*ai.ChatChoices{{Message: ai.NewAIMsgInfo(summary), FinishReason: "stop"}}, nil
}

func longestTextGist(infos []*ai.MsgInfo, limit int) string {
	best := ""
	for _, m := range infos {
		if m == nil {
			continue
		}
		if s, ok := m.Content.(string); ok && len(s) > len(best) {
			best = s
		}
	}
	best = strings.TrimSpace(strings.ReplaceAll(best, "\n", " "))
	return truncRunes(best, limit)
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// —— 真实会话解析：parts.jsonl 的 tool_start/tool_end 按 call_id 配对成工具轮 ——

type replayPart struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	CallID  string `json:"call_id"`
	Content string `json:"content"`
}

// loadRealToolRounds 从 parts.jsonl 还原真实工具轮，按 tool_start 出现顺序排列。
// 每轮 = assistant(tool_call, 真实 name+args) + tool(真实结果 content)。
func loadRealToolRounds(t *testing.T, partsPath string, maxRounds int) [][]*ai.MsgInfo {
	t.Helper()
	f, err := os.Open(partsPath)
	if err != nil {
		t.Skipf("无法打开会话 parts.jsonl（跳过真实回放）：%v", err)
	}
	defer f.Close()

	type startRec struct {
		name, callID, args string
	}
	var starts []startRec
	results := map[string]string{}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024) // 单行可达 ~72KB，放宽上限
	for sc.Scan() {
		var p replayPart
		if json.Unmarshal(sc.Bytes(), &p) != nil {
			continue
		}
		switch p.Type {
		case "tool_start":
			if len(starts) < maxRounds {
				starts = append(starts, startRec{name: p.Name, callID: p.CallID, args: p.Content})
			}
		case "tool_end":
			if _, seen := results[p.CallID]; !seen {
				results[p.CallID] = p.Content
			}
		}
	}

	rounds := make([][]*ai.MsgInfo, 0, len(starts))
	for _, s := range starts {
		res, ok := results[s.callID]
		if !ok || strings.TrimSpace(res) == "" {
			continue
		}
		tc := &ai.FunctionTool{
			Id:   s.callID,
			Type: "function",
			Function: &ai.FunctionDetail{
				Name:      firstNonEmpty(strings.TrimSpace(s.name), "tool"),
				Arguments: s.args,
			},
		}
		assistant := ai.NewAIMsgInfo("")
		assistant.ToolCalls = []*ai.FunctionTool{tc}
		rounds = append(rounds, []*ai.MsgInfo{
			assistant,
			ai.NewToolCallResultMsgInfo(res, s.callID),
		})
	}
	if len(rounds) == 0 {
		t.Skip("会话里没有可配对的工具轮，跳过")
	}
	return rounds
}

// resolveReplaySession 决定用哪个会话：ASTER_REPLAY_SESSION 优先（id 或 parts.jsonl 全路径），
// 否则自动挑 ~/.aster/sessions 下 parts.jsonl 最大的那个。找不到就 Skip。
func resolveReplaySession(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("ASTER_REPLAY_SESSION")); v != "" {
		if strings.HasSuffix(v, ".jsonl") {
			return v
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".aster", "sessions", v, "parts.jsonl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("无法定位 home 目录：%v", err)
	}
	root := filepath.Join(home, ".aster", "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("无 ~/.aster/sessions（跳过真实回放）：%v", err)
	}
	type cand struct {
		path string
		size int64
	}
	var cands []cand
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name(), "parts.jsonl")
		if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
			cands = append(cands, cand{p, fi.Size()})
		}
	}
	if len(cands) == 0 {
		t.Skip("~/.aster/sessions 下没有 parts.jsonl，跳过")
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].size > cands[j].size })
	return cands[0].path
}

func flattenRounds(rounds [][]*ai.MsgInfo) []*ai.MsgInfo {
	out := make([]*ai.MsgInfo, 0, len(rounds)*2)
	for _, r := range rounds {
		out = append(out, r...)
	}
	return out
}

// renderMsg 把一条消息渲染为单行摘要（用于打印最终骨架）。
func renderMsg(m *ai.MsgInfo, bodyLimit int) string {
	if m == nil {
		return "<nil>"
	}
	role := strings.TrimSpace(m.Role)
	if len(m.ToolCalls) > 0 {
		name := ""
		if m.ToolCalls[0].Function != nil {
			name = m.ToolCalls[0].Function.Name
		}
		return fmt.Sprintf("[%s tool_call name=%s id=%s]", role, name, m.ToolCalls[0].Id)
	}
	body, _ := m.Content.(string)
	tag := role
	if strings.TrimSpace(m.Type) != "" {
		tag += "/" + strings.TrimSpace(m.Type)
	}
	if strings.TrimSpace(m.ToolCallID) != "" {
		tag += " id=" + strings.TrimSpace(m.ToolCallID)
	}
	return fmt.Sprintf("[%s] %s", tag, truncRunes(strings.ReplaceAll(body, "\n", " "), bodyLimit))
}

func toolResultLen(m *ai.MsgInfo) int {
	if m == nil {
		return 0
	}
	if s, ok := m.Content.(string); ok {
		return len([]rune(s))
	}
	return 0
}

var replaySep = strings.Repeat("=", 78)

func TestRealSessionMultiRoundCompaction(t *testing.T) {
	partsPath := resolveReplaySession(t)
	rounds := loadRealToolRounds(t, partsPath, 400)
	t.Logf("回放会话：%s", partsPath)
	t.Logf("还原真实工具轮：%d 轮", len(rounds))

	t.Run("Layer1_就地缩短", func(t *testing.T) {
		testLayer1InPlaceShorten(t, rounds)
	})
	t.Run("Layer2_AI折叠多轮", func(t *testing.T) {
		testLayer2MultiRoundFold(t, rounds)
	})
}

// —— Layer 1：老 tool_result 就地截短（无 AI，结构不变）——
func testLayer1InPlaceShorten(t *testing.T, rounds [][]*ai.MsgInfo) {
	const maxRunes = 800
	// 取前若干轮真实转录；保留最近 1 轮，其余就地缩短。
	transcript := flattenRounds(rounds[:min(len(rounds), 8)])

	// 记录被缩短对象（最老一轮 tool_result）的原文与原长。
	oldestBeforeLen := toolResultLen(transcript[1])
	oldestBefore, _ := transcript[1].Content.(string)

	before := estimateHistoryTokens(transcript)
	out, did := shortenOldToolResults(transcript, 1, maxRunes)
	after := estimateHistoryTokens(out)

	shortened, _ := out[1].Content.(string)
	dir := replayDumpDir(t)
	beforePath := dumpFull(t, dir, "layer1_before_full.txt", oldestBefore)
	afterPath := dumpFull(t, dir, "layer1_after_full.txt", shortened)

	t.Logf("\n%s\nLayer 1 就地缩短（maxRunes=%d，保留最近 1 轮）\n%s", replaySep, maxRunes, replaySep)
	t.Logf("整体：%d → %d tokens（缩短发生=%v）", before, after, did)
	t.Logf("被缩短的最老 tool_result：%d runes → %d runes", oldestBeforeLen, toolResultLen(out[1]))
	t.Logf("全文 dump：\n  原文  → %s\n  压缩后 → %s", beforePath, afterPath)

	if !did {
		t.Fatalf("期望发生就地缩短")
	}
	if !strings.Contains(shortened, stepHistoryToolResultShortenedHint) {
		t.Fatalf("期望缩短后带预算提示尾注")
	}
	if err := validateToolCallSequence(out); err != nil {
		t.Fatalf("缩短后协议被破坏：%v", err)
	}
}

// —— Layer 2：整批老工具轮折叠成一条摘要（AI 摘要，模拟多轮反复触发）——
func testLayer2MultiRoundFold(t *testing.T, rounds [][]*ai.MsgInfo) {
	// 小窗口制造溢出：input=16000 / output=2000 → usable≈14000，触发线≈12600 tokens。
	client := &replayCompactionClient{inputLimit: 16000, outputLimit: 2000}
	budget := resolveContextBudget(client)
	trigger := int(float64(budget.UsableInputTokens) * 0.90)
	t.Logf("\n%s\nLayer 2 AI 折叠（usable=%d，触发线≈%d tokens）\n%s", replaySep, budget.UsableInputTokens, trigger, replaySep)

	// keepLastRounds=2；toolResultMaxRunes 设超大 → Layer 1 对纯文本近乎 no-op，
	// 强制走 Layer 2 AI 折叠，纯粹演示「N 轮工具输出 → 1 条摘要」。
	compactor := NewAIStepHistoryCompactor(0.90, 2, 1_000_000, nil)

	var transcript []*ai.MsgInfo
	next := 0
	for wave := 1; wave <= 4 && next < len(rounds); wave++ {
		for next < len(rounds) && estimateHistoryTokens(transcript) < trigger+6000 {
			transcript = append(transcript, rounds[next]...)
			next++
		}

		beforeTokens := estimateHistoryTokens(transcript)
		beforeRounds := len(splitToolRounds(transcript))
		beforeMsgs := len(transcript)
		callsBefore := client.chatExCalls
		// 本波压缩前的完整转录（全文，供 review）。
		beforeFull := renderTranscriptFull(transcript)

		res, err := compactor.Compact(context.Background(), client, "深度代码审计", "system-prompt", transcript)
		if err != nil {
			t.Fatalf("wave %d Compact 失败：%v", wave, err)
		}
		transcript = NormalizeHistoryMsgInfos(res.StepHistory)

		t.Logf("\n第 %d 波（累计消费真实工具轮 %d/%d）", wave, next, len(rounds))
		t.Logf("  触发前：%d msgs / %d 工具轮 / %d tokens", beforeMsgs, beforeRounds, beforeTokens)
		t.Logf("  压缩后：%d msgs / %d 工具轮 / %d tokens  (本波 Layer2 折叠 %d 次, CanContinue=%v)",
			len(transcript), len(splitToolRounds(transcript)), estimateHistoryTokens(transcript),
			client.chatExCalls-callsBefore, res.CanContinue)
		if beforeTokens > 0 {
			t.Logf("  压缩比：%.1f%% tokens 保留（省下 %d tokens）",
				float64(estimateHistoryTokens(transcript))*100/float64(beforeTokens), beforeTokens-estimateHistoryTokens(transcript))
		}
		if wave == 1 {
			dir := replayDumpDir(t)
			bp := dumpFull(t, dir, "layer2_wave1_before_full.txt", beforeFull)
			ap := dumpFull(t, dir, "layer2_wave1_after_full.txt", renderTranscriptFull(transcript))
			t.Logf("  全文 dump（wave1）：\n    压缩前 → %s\n    压缩后 → %s", bp, ap)
		}
	}

	// 最终转录全文 dump（压缩后完整内容，供 review）。
	finalPath := dumpFull(t, replayDumpDir(t), "layer2_final_full.txt", renderTranscriptFull(transcript))
	t.Logf("最终转录全文 dump → %s", finalPath)

	// 最终骨架（压缩后逐条）——展示摘要条与保留的最近若干轮。
	t.Logf("\n%s\n最终转录骨架（压缩后逐条）\n%s", replaySep, replaySep)
	for i, m := range transcript {
		t.Logf("%2d. %s", i, renderMsg(m, 140))
	}

	if client.chatExCalls == 0 {
		t.Fatalf("期望至少发生一次 Layer 2 折叠，但未调用 summarizer")
	}
	if err := validateToolCallSequence(transcript); err != nil {
		t.Fatalf("压缩后 tool_call/tool_result 协议被破坏：%v", err)
	}
}

// replayDumpDir 决定全文 dump 落点：ASTER_REPLAY_DUMP 优先，否则临时目录。
func replayDumpDir(t *testing.T) string {
	if v := strings.TrimSpace(os.Getenv("ASTER_REPLAY_DUMP")); v != "" {
		_ = os.MkdirAll(v, 0o755)
		return v
	}
	d := filepath.Join(os.TempDir(), "compaction_review")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func dumpFull(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写 dump 文件失败 %s：%v", p, err)
	}
	return p
}

// renderTranscriptFull 把整段转录逐条渲染为可读全文（不截断），供 review。
func renderTranscriptFull(msgs []*ai.MsgInfo) string {
	var b strings.Builder
	for i, m := range msgs {
		if m == nil {
			continue
		}
		role := strings.TrimSpace(m.Role)
		typ := strings.TrimSpace(m.Type)
		if len(m.ToolCalls) > 0 {
			name, args := "", ""
			if fn := m.ToolCalls[0].Function; fn != nil {
				name = fn.Name
				if s, ok := fn.Arguments.(string); ok {
					args = s
				}
			}
			fmt.Fprintf(&b, "#%02d [%s tool_call name=%s id=%s]\n  args: %s\n\n", i, role, name, m.ToolCalls[0].Id, args)
			continue
		}
		body, _ := m.Content.(string)
		hdr := "#%02d [%s"
		fmt.Fprintf(&b, hdr, i, role)
		if typ != "" {
			fmt.Fprintf(&b, "/%s", typ)
		}
		if id := strings.TrimSpace(m.ToolCallID); id != "" {
			fmt.Fprintf(&b, " id=%s", id)
		}
		fmt.Fprintf(&b, "] (%d runes)\n%s\n\n", len([]rune(body)), body)
	}
	return b.String()
}

func oldestToolRoundText(transcript []*ai.MsgInfo) string {
	for _, m := range transcript {
		if m == nil {
			continue
		}
		if strings.TrimSpace(m.Role) == "tool" {
			if s, ok := m.Content.(string); ok {
				return s
			}
		}
	}
	return ""
}
