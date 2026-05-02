package builtin_tools

import "fmt"

func ReadFileLargeTruncationMessage(thresholdBytes int64, previewBytes int, omittedBytes int64) string {
	if omittedBytes < 0 {
		omittedBytes = 0
	}
	return fmt.Sprintf(
		"文件超过 %dKB，已返回前 %d 字节预览，省略 %d 字节（内容末尾以 ... 标记）。请优先使用 rg 定位关键信息，再按需读取小文件。",
		thresholdBytes/1024,
		previewBytes,
		omittedBytes,
	)
}

func ReadFileTruncationMessage(maxBytes int64) string {
	if maxBytes > 0 {
		return fmt.Sprintf("read_file 内容已按 max_bytes=%d 截断，后续内容以 ... 省略。可使用 start_line/end_line 或更小 max_bytes 分段读取。", maxBytes)
	}
	return "read_file 内容已截断，后续内容以 ... 省略。可使用 start_line/end_line 或更小 max_bytes 分段读取。"
}

func RgTruncationMessage(captureLimitBytes int64) string {
	if captureLimitBytes > 0 {
		return fmt.Sprintf("rg 输出已按 capture_limit_bytes=%d 截断，后续内容以 ... 省略。请缩小 path、pattern、glob、type 或 context 后重试。", captureLimitBytes)
	}
	return "rg 输出已截断，后续内容以 ... 省略。请缩小 path、pattern、glob、type 或 context 后重试。"
}

func ListFilesTruncationMessage(maxOutputBytes int64) string {
	if maxOutputBytes <= 0 {
		return "list_files 输出已截断，后续条目以 ... 省略。建议缩小 path 或使用 include_exts/exclude_dirs 进一步过滤。"
	}
	return fmt.Sprintf("list_files 输出已按 max_output_bytes=%d 截断，后续条目以 ... 省略。建议缩小 path 或使用 include_exts/exclude_dirs 进一步过滤。", maxOutputBytes)
}
