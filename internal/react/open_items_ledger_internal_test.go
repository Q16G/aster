package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLedgerFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestRollupChildArtifactsToParentLedger(t *testing.T) {
	root := t.TempDir()
	parentShared := filepath.Join(root, "parent", "shared")
	childShared := filepath.Join(root, "child", "shared")

	writeLedgerFile(t, parentShared, "open_items.md",
		"# 未闭环账本\n\n## 未解决\n- [OI-001] 父级条目\n\n## 不可解局限\n\n## 待复核（子agent）\n")
	writeLedgerFile(t, childShared, "task_context.md",
		"# 贯穿全程关键事实\n\n## 输入事实\n- 地址: 10.0.0.1\n\n## 执行中补充\n- 子发现: 入口 B 可达\n")
	writeLedgerFile(t, childShared, "open_items.md",
		"## 未解决\n- 子未解决: 模块 Y 未深测\n\n## 不可解局限\n- 子受阻: 缺凭据\n")

	rolled, err := rollupChildArtifactsToParentLedger(parentShared, "sub-abc", childShared)
	if err != nil {
		t.Fatalf("rollup failed: %v", err)
	}
	if !rolled {
		t.Fatal("expected rollup to produce content")
	}

	data, err := os.ReadFile(filepath.Join(parentShared, "open_items.md"))
	if err != nil {
		t.Fatalf("read parent ledger: %v", err)
	}
	content := string(data)

	staging := extractMarkdownSection(content, openItemsStagingHeading)
	for _, want := range []string{"### 来源: sub-abc", "子发现: 入口 B 可达", "子未解决: 模块 Y 未深测", "子受阻: 缺凭据"} {
		if !strings.Contains(staging, want) {
			t.Fatalf("staging section missing %q, got:\n%s", want, staging)
		}
	}
	// 父级语义区不被触碰。
	if unresolved := extractMarkdownSection(content, openItemsUnresolvedHeading); !strings.Contains(unresolved, "OI-001") || strings.Contains(unresolved, "子未解决") {
		t.Fatalf("parent unresolved section should be untouched, got:\n%s", unresolved)
	}
}

func TestRollupChildArtifacts_NoContentNoWrite(t *testing.T) {
	root := t.TempDir()
	parentShared := filepath.Join(root, "parent", "shared")
	childShared := filepath.Join(root, "child", "shared")
	writeLedgerFile(t, parentShared, "open_items.md", "## 未解决\n\n## 不可解局限\n")
	writeLedgerFile(t, childShared, "open_items.md", "## 未解决\n\n## 不可解局限\n")

	rolled, err := rollupChildArtifactsToParentLedger(parentShared, "sub-x", childShared)
	if err != nil {
		t.Fatalf("rollup failed: %v", err)
	}
	if rolled {
		t.Fatal("expected no rollup for empty child sections")
	}
}

func TestAppendToOpenItemsStaging_RebuildsMissingSectionAndInsertsBeforeNextHeading(t *testing.T) {
	dir := t.TempDir()
	// 暂存区不是末节的旧序文件：插入应落在暂存区内、下一标题之前。
	p := writeLedgerFile(t, dir, "open_items.md",
		"## 待复核（子agent）\n\n### 来源: sub-old\n- 旧条目\n\n## 未解决\n- [OI-002] x\n")
	if err := appendToOpenItemsStaging(p, "### 来源: sub-new\n- 新条目"); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, _ := os.ReadFile(p)
	content := string(data)
	staging := extractMarkdownSection(content, openItemsStagingHeading)
	if !strings.Contains(staging, "sub-old") || !strings.Contains(staging, "sub-new") {
		t.Fatalf("expected both blocks in staging, got:\n%s", staging)
	}
	if unresolved := extractMarkdownSection(content, openItemsUnresolvedHeading); strings.Contains(unresolved, "sub-new") {
		t.Fatalf("new block leaked into unresolved section:\n%s", unresolved)
	}

	// 模型覆盖写丢掉暂存区后：append 应在文件尾重建节。
	p2 := writeLedgerFile(t, dir, "open_items2.md", "## 未解决\n- a\n\n## 不可解局限\n")
	if err := appendToOpenItemsStaging(p2, "### 来源: sub-z\n- 条目"); err != nil {
		t.Fatalf("append rebuild: %v", err)
	}
	data2, _ := os.ReadFile(p2)
	if got := extractMarkdownSection(string(data2), openItemsStagingHeading); !strings.Contains(got, "sub-z") {
		t.Fatalf("expected rebuilt staging section, got:\n%s", string(data2))
	}
}

// TestAppendToMarkdownSection_HeadingLineAnchored 锁定锚点行首化：账本条目正文里出现的
// `## 待复核（子agent）` 字面不应被当作节标题命中，避免插入落到错误区段中央。
func TestAppendToMarkdownSection_HeadingLineAnchored(t *testing.T) {
	dir := t.TempDir()
	// `## 未解决` 区有条目引用了 `## 待复核（子agent）` 字面（行内出现，非行首独立节标题），
	// 真正的 `## 待复核（子agent）` 节标题位于文件末尾——append 必须落在末节内，不能错位
	// 到未解决区内（旧版 strings.Index 子串匹配会错位到条目所在行尾）。
	p := writeLedgerFile(t, dir, "open_items.md",
		"## 未解决\n- [OI-007] 详见 ## 待复核（子agent） 暂存区\n\n## 不可解局限\n\n## 待复核（子agent）\n")
	if err := appendToOpenItemsStaging(p, "### 来源: sub-x\n- 新条目"); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, _ := os.ReadFile(p)
	content := string(data)

	staging := extractMarkdownSection(content, openItemsStagingHeading)
	if !strings.Contains(staging, "sub-x") || !strings.Contains(staging, "新条目") {
		t.Fatalf("new block must land in 待复核 section, got staging:\n%s\nfull:\n%s", staging, content)
	}
	// 未解决区不被污染：OI-007 条目原样保留，新条目不应漏进来。
	unresolved := extractMarkdownSection(content, openItemsUnresolvedHeading)
	if !strings.Contains(unresolved, "OI-007") {
		t.Fatalf("unresolved must keep OI-007 entry, got:\n%s", unresolved)
	}
	if strings.Contains(unresolved, "sub-x") || strings.Contains(unresolved, "新条目") {
		t.Fatalf("new block leaked into unresolved section:\n%s", unresolved)
	}
	// 不可解局限区原样未触达。
	blocked := extractMarkdownSection(content, openItemsBlockedHeading)
	if strings.Contains(blocked, "sub-x") {
		t.Fatalf("new block leaked into blocked section:\n%s", blocked)
	}
}
