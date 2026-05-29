package builtin_tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

// CommandRunResult 命令执行结果
type CommandRunResult struct {
	Args            []string
	ExitCode        int
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	RunErr          error
}

// LimitedWriter 有界输出捕获器，防止 OOM
type LimitedWriter struct {
	max       int64
	buf       []byte
	truncated bool
}

func (w *LimitedWriter) Write(p []byte) (int, error) {
	if w == nil || w.max <= 0 {
		return len(p), nil
	}
	remain := w.max - int64(len(w.buf))
	if remain <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remain {
		w.buf = append(w.buf, p[:remain]...)
		w.truncated = true
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// graceKillDelay 取消命令时，先对进程组发 SIGTERM，宽限期内未退出再整组 SIGKILL。
// 给 semgrep-core 等子进程留出落盘 / 清理临时文件的时间。
const graceKillDelay = 3 * time.Second

// procSignal 是跨平台的进程组清理信号抽象，由各平台实现 killGroup 解释。
type procSignal int

const (
	termSignal procSignal = iota // 优雅终止
	killSignal                   // 强制终止
)

// RunCommandLimited 在指定目录中执行命令，限制 stdout/stderr 大小。
// 命令在独立进程组中启动；ctx 取消时对整个进程组优雅降级清理
// （SIGTERM → 宽限期 → SIGKILL），避免 shell 之下的孙子进程（如 semgrep-core）孤儿残留。
func RunCommandLimited(ctx context.Context, dir, exe string, args []string, maxStdout, maxStderr int64, waitDelay time.Duration) *CommandRunResult {
	cmd := exec.CommandContext(ctx, exe, args...)
	setProcGroup(cmd)
	done := make(chan struct{})
	cmd.Cancel = func() error {
		killGroup(cmd, termSignal)
		go func() {
			select {
			case <-done:
			case <-time.After(graceKillDelay):
				killGroup(cmd, killSignal)
			}
		}()
		return nil
	}
	if waitDelay > 0 {
		cmd.WaitDelay = waitDelay
	}
	cmd.Dir = dir
	stdout := &LimitedWriter{max: maxStdout}
	stderr := &LimitedWriter{max: maxStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	close(done)

	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return &CommandRunResult{
		Args:            append([]string{}, args...),
		ExitCode:        exitCode,
		Stdout:          string(stdout.buf),
		Stderr:          string(stderr.buf),
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
		RunErr:          err,
	}
}

// ValidateCommandResult 校验命令执行结果
func ValidateCommandResult(toolName string, res *CommandRunResult, okExitCodes ...int) error {
	if res == nil {
		return fmt.Errorf("%s returned no result", toolName)
	}
	for _, code := range okExitCodes {
		if res.ExitCode == code {
			return nil
		}
	}
	if res.RunErr != nil {
		stderr := strings.TrimSpace(TrimCapturedText(res.Stderr, res.StderrTruncated))
		if stderr != "" {
			return fmt.Errorf("%s command failed (exit=%d): %s", toolName, res.ExitCode, stderr)
		}
		return fmt.Errorf("%s command failed: %w", toolName, res.RunErr)
	}
	stderr := strings.TrimSpace(TrimCapturedText(res.Stderr, res.StderrTruncated))
	if stderr != "" {
		return fmt.Errorf("%s command failed (exit=%d): %s", toolName, res.ExitCode, stderr)
	}
	return fmt.Errorf("%s command failed with exit=%d", toolName, res.ExitCode)
}

// TrimCapturedText 对截断文本追加省略标记
func TrimCapturedText(text string, truncated bool) string {
	if !truncated {
		return text
	}
	return AppendTruncationMarker(text)
}

// TruncateHeadTail 保留头部和尾部，中间截断。尾部优先保留更多内容。
// headRatio 表示头部占总保留量的比例（建议 0.2-0.3）。
func TruncateHeadTail(content string, maxBytes int64, headRatio float64) (result string, truncated bool) {
	if int64(len(content)) <= maxBytes {
		return content, false
	}
	if headRatio < 0.05 {
		headRatio = 0.05
	}
	if headRatio > 0.5 {
		headRatio = 0.5
	}

	headSize := int64(float64(maxBytes) * headRatio)
	tailSize := maxBytes - headSize
	marker := "\n\n... [truncated: output exceeded limit, showing head + tail] ...\n\n"
	markerLen := int64(len(marker))

	if headSize+tailSize+markerLen > maxBytes {
		tailSize = maxBytes - headSize - markerLen
		if tailSize < 0 {
			tailSize = 0
		}
	}

	head := content[:headSize]
	tail := content[int64(len(content))-tailSize:]

	return head + marker + tail, true
}

// ------- Shell 检测 -------

// DetectShell 检测用户默认 shell
func DetectShell() (shellPath string, family ShellFamily) {
	if runtime.GOOS == "windows" {
		return detectShellWindows()
	}
	return detectShellPosix()
}

func detectShellPosix() (string, ShellFamily) {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh, ShellFamilyPosix
	}
	return "/bin/bash", ShellFamilyPosix
}

func detectShellWindows() (string, ShellFamily) {
	if p := findGitBash(); p != "" {
		return p, ShellFamilyPosix
	}
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, ShellFamilyPowerShell
		}
	}
	if p, err := exec.LookPath("cmd.exe"); err == nil {
		return p, ShellFamilyCmd
	}
	return "cmd.exe", ShellFamilyCmd
}

func findGitBash() string {
	if p := os.Getenv("ASTER_GIT_BASH_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	out, err := exec.Command("where.exe", "git").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			gitPath := strings.TrimSpace(lines[0])
			bashPath := filepath.Join(filepath.Dir(filepath.Dir(gitPath)), "bin", "bash.exe")
			if _, err := os.Stat(bashPath); err == nil {
				return bashPath
			}
		}
	}
	for _, p := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\bash.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// FindPowerShell locates the PowerShell executable, preferring Core (pwsh) over Desktop (powershell).
func FindPowerShell() (path string, edition PSEdition) {
	for _, name := range []string{"pwsh.exe", "pwsh", "powershell.exe", "powershell"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, DetectPSEdition(p)
		}
	}
	return "", PSEditionUnknown
}

// DetectPSEdition determines PowerShell edition from the executable name.
func DetectPSEdition(shellPath string) PSEdition {
	base := shellPath
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.ToLower(base)
	base = strings.TrimSuffix(base, ".exe")
	if base == "pwsh" {
		return PSEditionCore
	}
	if base == "powershell" {
		return PSEditionDesktop
	}
	return PSEditionUnknown
}

// BuildShellCommand 根据 shell 家族构造命令参数
func BuildShellCommand(shellPath string, family ShellFamily, command string) (exe string, args []string) {
	switch family {
	case ShellFamilyPowerShell:
		utf8Prefix := "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; "
		return shellPath, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", utf8Prefix + command}
	case ShellFamilyCmd:
		return shellPath, []string{"/d", "/s", "/c", command}
	default:
		return shellPath, []string{"-lc", command}
	}
}

// ------- 返回码语义解释 -------

// InterpretExitCode 根据命令名称和退出码给出语义解释
func InterpretExitCode(command string, exitCode int) string {
	if exitCode == 0 {
		return "success"
	}

	cmdBase := extractCommandBase(command)

	if exitCode == 1 {
		switch cmdBase {
		case "grep", "rg", "egrep", "fgrep":
			return "No matches found"
		case "diff":
			return "Files differ"
		case "git":
			if containsGitDiffFlag(command) {
				return "Working tree differs"
			}
		case "test", "[":
			return "Condition evaluated to false"
		}
	}

	switch cmdBase {
	case "go":
		if strings.Contains(command, "go test") {
			return "Tests failed or package build/test failed"
		}
	case "npm", "pnpm", "yarn":
		if strings.Contains(command, " test") {
			return "Test script failed"
		}
	case "make":
		return "Target or subcommand failed"
	case "curl":
		return interpretCurlExitCode(exitCode)
	case "robocopy":
		if exitCode <= 7 {
			return "success (robocopy bitfield: files copied)"
		}
		return fmt.Sprintf("robocopy error (exit code %d, bitfield >= 8)", exitCode)
	case "pwsh", "powershell":
		return "PowerShell command or script failed"
	case "cmd":
		return "Command interpreter or subcommand failed"
	}

	return fmt.Sprintf("error (exit code %d)", exitCode)
}

func extractCommandBase(command string) string {
	cmd := strings.TrimSpace(command)
	// 跳过环境变量赋值前缀
	for strings.Contains(cmd, "=") {
		parts := strings.SplitN(cmd, " ", 2)
		if len(parts) < 2 || !strings.Contains(parts[0], "=") {
			break
		}
		cmd = strings.TrimSpace(parts[1])
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	base := fields[0]
	// 取路径末段
	if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
		base = base[idx+1:]
	}
	return strings.ToLower(base)
}

func containsGitDiffFlag(command string) bool {
	return strings.Contains(command, "diff --exit-code") || strings.Contains(command, "diff --quiet")
}

func interpretCurlExitCode(code int) string {
	switch code {
	case 6:
		return "Could not resolve host"
	case 7:
		return "Failed to connect to host"
	case 22:
		return "HTTP response indicated error (4xx/5xx)"
	case 28:
		return "Operation timed out"
	case 35:
		return "SSL/TLS connect error"
	default:
		return fmt.Sprintf("curl failed (exit code %d)", code)
	}
}

// ------- Windows 路径转换 -------

// WindowsPathToPosix converts a Windows path to POSIX style for Git Bash.
func WindowsPathToPosix(winPath string) string {
	if len(winPath) >= 2 && isASCIILetter(winPath[0]) && winPath[1] == ':' {
		drive := strings.ToLower(string(winPath[0]))
		rest := strings.ReplaceAll(winPath[2:], `\`, "/")
		return "/" + drive + rest
	}
	if strings.HasPrefix(winPath, `\\`) {
		return "/" + strings.ReplaceAll(winPath[1:], `\`, "/")
	}
	return strings.ReplaceAll(winPath, `\`, "/")
}

// PosixPathToWindows converts a POSIX path from Git Bash/Cygwin to Windows style.
func PosixPathToWindows(posixPath string) string {
	if strings.HasPrefix(posixPath, "/cygdrive/") && len(posixPath) >= 11 && isASCIILetter(posixPath[10]) {
		drive := strings.ToUpper(string(posixPath[10]))
		rest := strings.ReplaceAll(posixPath[11:], "/", `\`)
		return drive + ":" + rest
	}
	if len(posixPath) >= 3 && posixPath[0] == '/' && isASCIILetter(posixPath[1]) && posixPath[2] == '/' {
		drive := strings.ToUpper(string(posixPath[1]))
		rest := strings.ReplaceAll(posixPath[2:], "/", `\`)
		return drive + ":" + rest
	}
	if strings.HasPrefix(posixPath, "//") {
		return `\` + strings.ReplaceAll(posixPath[1:], "/", `\`)
	}
	return posixPath
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// ------- UTF-16LE 编码 -------

// EncodeUTF16LEBase64 encodes a string to base64 UTF-16LE for PowerShell -EncodedCommand.
func EncodeUTF16LEBase64(s string) string {
	units := utf16.Encode([]rune(s))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		buf[i*2] = byte(u)
		buf[i*2+1] = byte(u >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
