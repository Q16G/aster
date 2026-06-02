package react

import (
	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type toolResultRender struct {
	Content any
	Media   []builtin_tools.ToolResultMedia
}

type readFileImageRenderPayload struct {
	OK               bool   `json:"ok"`
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	MIMEType         string `json:"mime_type"`
	SizeBytes        int64  `json:"size_bytes"`
	Width            int    `json:"width,omitempty"`
	Height           int    `json:"height,omitempty"`
	ContextAvailable bool   `json:"context_available"`
	Message          string `json:"message,omitempty"`
}

func buildToolResultRender(toolName string, rawOutput string) toolResultRender {
	render := toolResultRender{Content: rawOutput}
	if strings.TrimSpace(rawOutput) == "" {
		return render
	}
	if strings.TrimSpace(toolName) != builtin_tools.ReadFileToolName {
		return render
	}
	return buildReadFileToolResultRender(rawOutput)
}

func buildReadFileToolResultRender(rawOutput string) toolResultRender {
	var payload readFileImageRenderPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(rawOutput)), &payload); err != nil {
		return toolResultRender{Content: rawOutput}
	}
	if strings.TrimSpace(payload.Kind) != "image" || strings.TrimSpace(payload.Path) == "" {
		return toolResultRender{Content: rawOutput}
	}

	summary := formatReadFileImageSummary(payload)
	contexts := []*ai.ChatContext{ai.NewBaseChat(summary)}
	if payload.ContextAvailable {
		dataURL, err := buildImageDataURL(payload.Path, payload.MIMEType)
		if err == nil {
			contexts = append(contexts, ai.NewImageChat(dataURL))
		} else {
			contexts[0].Text = fmt.Sprintf("%s 未能附加视觉上下文：%s", summary, err.Error())
		}
	}

	media := []builtin_tools.ToolResultMedia{{
		Kind:      "image",
		Path:      payload.Path,
		MIMEType:  payload.MIMEType,
		SizeBytes: payload.SizeBytes,
		Width:     payload.Width,
		Height:    payload.Height,
	}}
	return toolResultRender{
		Content: contexts,
		Media:   media,
	}
}

func formatReadFileImageSummary(payload readFileImageRenderPayload) string {
	parts := []string{
		fmt.Sprintf("read_file 图片：%s", strings.TrimSpace(payload.Path)),
	}

	meta := make([]string, 0, 3)
	if payload.MIMEType != "" {
		meta = append(meta, payload.MIMEType)
	}
	if payload.SizeBytes > 0 {
		meta = append(meta, fmt.Sprintf("%d bytes", payload.SizeBytes))
	}
	if payload.Width > 0 && payload.Height > 0 {
		meta = append(meta, fmt.Sprintf("%dx%d", payload.Width, payload.Height))
	}
	if len(meta) > 0 {
		parts = append(parts, fmt.Sprintf("(%s)", strings.Join(meta, ", ")))
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		parts = append(parts, msg)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func buildImageDataURL(path string, mimeType string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取图片失败: %w", err)
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
		return "", fmt.Errorf("不支持的图片 MIME 类型: %s", mimeType)
	}
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)), nil
}

func finalizeToolResultContent(content any, errText string) any {
	trimmedErr := strings.TrimSpace(errText)
	if trimmedErr == "" {
		if content == nil {
			return ""
		}
		return content
	}

	errBlock := fmt.Sprintf("Error: %s", trimmedErr)
	switch typed := content.(type) {
	case nil:
		return errBlock
	case string:
		if strings.TrimSpace(typed) == "" {
			return errBlock
		}
		return fmt.Sprintf("%s\n\n%s", typed, errBlock)
	case []*ai.ChatContext:
		merged := make([]*ai.ChatContext, 0, len(typed)+1)
		merged = append(merged, typed...)
		merged = append(merged, ai.NewBaseChat(errBlock))
		return merged
	default:
		text := strings.TrimSpace(FormatMsgContent(content))
		if text == "" {
			return errBlock
		}
		return fmt.Sprintf("%s\n\n%s", text, errBlock)
	}
}
