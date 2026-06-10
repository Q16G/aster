package react

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	openItemsFileName            = "open_items.md"
	openItemsStagingHeading      = "## 待复核（子agent）"
	taskContextRuntimeAddHeading = "## 执行中补充"
	openItemsUnresolvedHeading   = "## 未解决"
	openItemsBlockedHeading      = "## 不可解局限"
)

// extractMarkdownSection 返回 content 中 heading（## 级）节的正文（去首尾空白）。
func extractMarkdownSection(content, heading string) string {
	lines := strings.Split(content, "\n")
	var out []string
	in := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			in = trimmed == heading
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// appendToMarkdownSection 把 block 追加进文件中 heading（## 级）节的末尾（下一个
// ## 级标题之前）。节不存在时（如被模型覆盖写丢弃）在文件尾重建，保证机械写入永不丢数据。
func appendToMarkdownSection(path, heading, block string) error {
	block = strings.TrimSpace(block)
	if block == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)

	idx := strings.Index(content, heading)
	if idx < 0 {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + heading + "\n"
		idx = strings.Index(content, heading)
	}

	// 插入点：节内、下一个 ## 级标题之前（该节可能不是末节）。
	sectionStart := idx + len(heading)
	insertAt := len(content)
	if next := strings.Index(content[sectionStart:], "\n## "); next >= 0 {
		insertAt = sectionStart + next + 1 // 保留换行，块插在下一标题行之前
	}

	var b strings.Builder
	b.WriteString(content[:insertAt])
	if !strings.HasSuffix(content[:insertAt], "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n" + block + "\n")
	b.WriteString(content[insertAt:])
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// appendToOpenItemsStaging 把 block 追加进父级账本的「## 待复核（子agent）」暂存区。
func appendToOpenItemsStaging(openItemsPath, block string) error {
	return appendToMarkdownSection(openItemsPath, openItemsStagingHeading, block)
}

// allocateLedgerID 从账本头部 next_id 计数器取号并递增写回；计数器行缺失（被模型
// 覆盖写丢弃）时扫描既有 [OI-n] 取 max+1 重建。
func allocateLedgerID(openItemsPath string) (string, error) {
	data, err := os.ReadFile(openItemsPath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	next := 0
	counterLine := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "next_id:") {
			if n, perr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, "next_id:"))); perr == nil && n > 0 {
				next = n
				counterLine = i
			}
			break
		}
	}
	if next <= 0 {
		maxSeen := 0
		for _, m := range ledgerIDRe.FindAllStringSubmatch(content, -1) {
			if n, perr := strconv.Atoi(m[1]); perr == nil && n > maxSeen {
				maxSeen = n
			}
		}
		next = maxSeen + 1
	}

	id := fmt.Sprintf("OI-%03d", next)
	counter := fmt.Sprintf("next_id: %d", next+1)
	if counterLine >= 0 {
		lines[counterLine] = counter
		content = strings.Join(lines, "\n")
	} else {
		content = "# 未闭环账本\n" + counter + "\n\n" + content
	}
	if err := os.WriteFile(openItemsPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return id, nil
}

var ledgerIDRe = regexp.MustCompile(`\[OI-(\d+)\]`)

// removeLedgerLine 从账本中移除含指定 OI-id 的整行（含其紧随的缩进续行），返回被移除文本。
func removeLedgerLine(openItemsPath, oiID string) (string, bool, error) {
	data, err := os.ReadFile(openItemsPath)
	if err != nil {
		return "", false, err
	}
	lines := strings.Split(string(data), "\n")
	marker := "[" + strings.TrimSpace(oiID) + "]"
	var removed []string
	out := make([]string, 0, len(lines))
	i := 0
	for i < len(lines) {
		if strings.Contains(lines[i], marker) {
			removed = append(removed, lines[i])
			i++
			// 连带缩进续行
			for i < len(lines) && (strings.HasPrefix(lines[i], "  ") || strings.HasPrefix(lines[i], "\t")) {
				removed = append(removed, lines[i])
				i++
			}
			continue
		}
		out = append(out, lines[i])
		i++
	}
	if len(removed) == 0 {
		return "", false, nil
	}
	if err := os.WriteFile(openItemsPath, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return "", false, err
	}
	return strings.Join(removed, "\n"), true, nil
}

// annotateLedgerLine 在含指定 OI-id 的行尾追加更新批注。
func annotateLedgerLine(openItemsPath, oiID, note string) (bool, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return false, nil
	}
	data, err := os.ReadFile(openItemsPath)
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(data), "\n")
	marker := "[" + strings.TrimSpace(oiID) + "]"
	found := false
	for i, line := range lines {
		if strings.Contains(line, marker) {
			lines[i] = line + "（更新: " + note + "）"
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	if err := os.WriteFile(openItemsPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// rollupChildArtifactsToParentLedger 在子 agent 终态时，把其 task_context.md
// 「## 执行中补充」节与 open_items.md「## 未解决」「## 不可解局限」条目全量按节
// 追加到父级账本暂存区（带来源标注）。纯机械 append、不做语义判断；语义归并
// （OI-id 取号 / 去重 / 裁剪）由 think_act 在下一次 step 收尾完成。
// 返回是否产生了回流内容。
func rollupChildArtifactsToParentLedger(parentSharedDir, childName, childSharedDir string) (bool, error) {
	if parentSharedDir == "" || childSharedDir == "" {
		return false, nil
	}

	var sections []string
	if data, err := os.ReadFile(filepath.Join(childSharedDir, "task_context.md")); err == nil {
		if body := extractMarkdownSection(string(data), taskContextRuntimeAddHeading); body != "" {
			sections = append(sections, "执行中补充:\n"+body)
		}
	}
	if data, err := os.ReadFile(filepath.Join(childSharedDir, openItemsFileName)); err == nil {
		content := string(data)
		if body := extractMarkdownSection(content, openItemsUnresolvedHeading); body != "" {
			sections = append(sections, "未解决:\n"+body)
		}
		if body := extractMarkdownSection(content, openItemsBlockedHeading); body != "" {
			sections = append(sections, "不可解局限:\n"+body)
		}
	}
	if len(sections) == 0 {
		return false, nil
	}

	block := fmt.Sprintf("### 来源: %s\n\n%s", strings.TrimSpace(childName), strings.Join(sections, "\n\n"))
	if err := appendToOpenItemsStaging(filepath.Join(parentSharedDir, openItemsFileName), block); err != nil {
		return false, err
	}
	return true, nil
}
