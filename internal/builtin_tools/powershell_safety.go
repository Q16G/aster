package builtin_tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:embed powershell_parser.ps1
var psParserScript string

// PSParseResult is the JSON output from the embedded PowerShell AST parser.
type PSParseResult struct {
	Status   string     `json:"status"`
	Commands [][]string `json:"commands"`
}

// PSDangerFlag represents a detected dangerous pattern in a PowerShell command.
type PSDangerFlag struct {
	Category string
	Command  string
	Reason   string
}

const (
	psCommandMaxBytes   = 1 << 20 // 1 MB
	psParserTimeout     = 10 * time.Second
)

// ParsePSCommandAST invokes the PowerShell AST parser to extract structured command sequences.
func ParsePSCommandAST(ctx context.Context, psPath, command string) (*PSParseResult, error) {
	if len(command) > psCommandMaxBytes {
		return &PSParseResult{Status: "parse_failed"}, nil
	}
	encodedScript := EncodeUTF16LEBase64(psParserScript)
	encodedCommand := EncodeUTF16LEBase64(command)

	parseCtx, cancel := context.WithTimeout(ctx, psParserTimeout)
	defer cancel()

	cmd := exec.CommandContext(parseCtx, psPath, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedScript)
	cmd.Env = append(os.Environ(), "ASTER_PS_PAYLOAD="+encodedCommand)

	output, err := cmd.Output()
	if err != nil {
		return &PSParseResult{Status: "parse_failed"}, nil
	}

	var result PSParseResult
	if err := json.Unmarshal(output, &result); err != nil {
		return &PSParseResult{Status: "parse_failed"}, nil
	}
	return &result, nil
}

// DetectPSDangerousPatterns checks AST-extracted command sequences for dangerous patterns.
func DetectPSDangerousPatterns(parsed *PSParseResult) []PSDangerFlag {
	if parsed == nil || parsed.Status != "ok" || len(parsed.Commands) == 0 {
		return nil
	}

	var flags []PSDangerFlag
	for _, argv := range parsed.Commands {
		if len(argv) == 0 {
			continue
		}
		cmdName := resolvePSAlias(strings.ToLower(argv[0]))
		params := lowerSlice(argv[1:])

		if f := checkCodeExec(cmdName, params); f != nil {
			flags = append(flags, *f)
		}
		if f := checkPrivilegeEscalation(cmdName, params); f != nil {
			flags = append(flags, *f)
		}
		if f := checkModuleInstall(cmdName); f != nil {
			flags = append(flags, *f)
		}
		if f := checkNestedPS(cmdName, params); f != nil {
			flags = append(flags, *f)
		}
		if f := checkWMIProcessCreate(cmdName, params); f != nil {
			flags = append(flags, *f)
		}
		if f := checkCommandHijack(cmdName); f != nil {
			flags = append(flags, *f)
		}
		if f := checkScheduledTask(cmdName); f != nil {
			flags = append(flags, *f)
		}
		if f := checkRemoteAccess(cmdName); f != nil {
			flags = append(flags, *f)
		}
		if f := checkSystemModification(cmdName); f != nil {
			flags = append(flags, *f)
		}
		if f := checkLOLBin(cmdName, params); f != nil {
			flags = append(flags, *f)
		}
	}

	if f := checkDownloadCradle(parsed.Commands); f != nil {
		flags = append(flags, *f)
	}

	return flags
}

func checkCodeExec(cmdName string, params []string) *PSDangerFlag {
	switch cmdName {
	case "invoke-expression":
		return &PSDangerFlag{"code_exec", "Invoke-Expression", "arbitrary code execution"}
	case "add-type":
		return &PSDangerFlag{"code_exec", "Add-Type", ".NET code compilation at runtime"}
	}
	if cmdName == "new-object" && containsParam(params, "-comobject") {
		return &PSDangerFlag{"code_exec", "New-Object -ComObject", "COM object instantiation"}
	}
	if cmdName == "invoke-item" || cmdName == "invoke-command" {
		return &PSDangerFlag{"code_exec", cmdName, "indirect code execution"}
	}
	return nil
}

func checkPrivilegeEscalation(cmdName string, params []string) *PSDangerFlag {
	if cmdName == "start-process" && containsParam(params, "-verb") && containsValue(params, "runas") {
		return &PSDangerFlag{"privilege_escalation", "Start-Process -Verb RunAs", "UAC elevation"}
	}
	return nil
}

func checkModuleInstall(cmdName string) *PSDangerFlag {
	switch cmdName {
	case "install-module", "install-script", "save-module", "save-script":
		return &PSDangerFlag{"module_install", cmdName, "installs code from remote repository"}
	}
	return nil
}

func checkNestedPS(cmdName string, params []string) *PSDangerFlag {
	if cmdName == "pwsh" || cmdName == "pwsh.exe" || cmdName == "powershell" || cmdName == "powershell.exe" {
		if containsParam(params, "-encodedcommand") || containsParam(params, "-e") || containsParam(params, "-enc") {
			return &PSDangerFlag{"nested_ps", cmdName, "encoded command bypasses detection"}
		}
		return &PSDangerFlag{"nested_ps", cmdName, "nested PowerShell process"}
	}
	return nil
}

func checkWMIProcessCreate(cmdName string, params []string) *PSDangerFlag {
	if cmdName == "invoke-wmimethod" || cmdName == "invoke-cimmethod" {
		hasProcessClass := false
		hasCreate := false
		for _, p := range params {
			if strings.Contains(p, "win32_process") {
				hasProcessClass = true
			}
			if strings.Contains(p, "create") {
				hasCreate = true
			}
		}
		if hasProcessClass && hasCreate {
			return &PSDangerFlag{"wmi_process", cmdName, "WMI process creation"}
		}
	}
	return nil
}

func checkCommandHijack(cmdName string) *PSDangerFlag {
	switch cmdName {
	case "set-alias", "new-alias":
		return &PSDangerFlag{"command_hijack", cmdName, "command resolution hijacking"}
	}
	return nil
}

func checkScheduledTask(cmdName string) *PSDangerFlag {
	switch cmdName {
	case "register-scheduledtask", "new-scheduledtask":
		return &PSDangerFlag{"persistence", cmdName, "scheduled task creation"}
	case "schtasks":
		return &PSDangerFlag{"persistence", "schtasks", "scheduled task creation"}
	}
	return nil
}

func checkDownloadCradle(commands [][]string) *PSDangerFlag {
	hasDownload := false
	hasExec := false
	for _, argv := range commands {
		if len(argv) == 0 {
			continue
		}
		cmd := resolvePSAlias(strings.ToLower(argv[0]))
		switch cmd {
		case "invoke-webrequest", "invoke-restmethod", "start-bitstransfer":
			hasDownload = true
		case "invoke-expression":
			hasExec = true
		case "certutil":
			if containsParam(lowerSlice(argv[1:]), "-urlcache") {
				hasDownload = true
			}
		}
	}
	if hasDownload && hasExec {
		return &PSDangerFlag{"download_cradle", "download+exec", "remote code download and execution"}
	}
	return nil
}

func checkRemoteAccess(cmdName string) *PSDangerFlag {
	switch cmdName {
	case "enable-psremoting":
		return &PSDangerFlag{"remote_access", "Enable-PSRemoting", "enables WinRM remote management"}
	case "enter-pssession", "new-pssession":
		return &PSDangerFlag{"remote_access", cmdName, "remote PowerShell session"}
	}
	return nil
}

func checkSystemModification(cmdName string) *PSDangerFlag {
	switch cmdName {
	case "set-executionpolicy":
		return &PSDangerFlag{"system_modification", "Set-ExecutionPolicy", "modifies script execution policy"}
	case "new-service", "set-service":
		return &PSDangerFlag{"system_modification", cmdName, "Windows service creation or modification"}
	case "new-netfirewallrule", "set-netfirewallrule", "remove-netfirewallrule":
		return &PSDangerFlag{"system_modification", cmdName, "firewall rule modification"}
	case "unblock-file":
		return &PSDangerFlag{"system_modification", "Unblock-File", "removes Zone.Identifier ADS mark-of-the-web"}
	}
	return nil
}

func checkLOLBin(cmdName string, params []string) *PSDangerFlag {
	switch cmdName {
	case "rundll32", "rundll32.exe":
		return &PSDangerFlag{"lolbin", "rundll32", "indirect code execution via DLL"}
	case "regsvr32", "regsvr32.exe":
		return &PSDangerFlag{"lolbin", "regsvr32", "COM server registration / remote SCT execution"}
	case "mshta", "mshta.exe":
		return &PSDangerFlag{"lolbin", "mshta", "HTML Application execution"}
	case "cscript", "cscript.exe", "wscript", "wscript.exe":
		return &PSDangerFlag{"lolbin", cmdName, "Windows Script Host execution"}
	case "certutil", "certutil.exe":
		if containsParam(params, "-decode") || containsParam(params, "-decodehex") {
			return &PSDangerFlag{"lolbin", "certutil", "binary decode and extraction"}
		}
	case "bitsadmin", "bitsadmin.exe":
		return &PSDangerFlag{"lolbin", "bitsadmin", "BITS transfer (potential download)"}
	}
	return nil
}

var psAliasMap = map[string]string{
	"iex":  "invoke-expression",
	"iwr":  "invoke-webrequest",
	"irm":  "invoke-restmethod",
	"saps": "start-process",
	"sal":  "set-alias",
	"nal":  "new-alias",
	"ii":   "invoke-item",
	"icm":  "invoke-command",
	"gci":  "get-childitem",
	"ri":   "remove-item",
	"del":  "remove-item",
	"rm":   "remove-item",
	"ni":   "new-item",
	"sp":   "set-itemproperty",
	"sc":   "set-content",
	"nsn":  "new-pssession",
	"etsn": "enter-pssession",
}

func resolvePSAlias(name string) string {
	if resolved, ok := psAliasMap[name]; ok {
		return resolved
	}
	return name
}

func containsParam(params []string, param string) bool {
	for _, p := range params {
		if strings.EqualFold(p, param) {
			return true
		}
	}
	return false
}

func containsValue(params []string, value string) bool {
	for _, p := range params {
		if strings.EqualFold(p, value) {
			return true
		}
	}
	return false
}

func lowerSlice(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = strings.ToLower(v)
	}
	return out
}
