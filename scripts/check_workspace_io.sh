#!/usr/bin/env bash
# check_workspace_io.sh —— M6 收口的 workspace IO 架构守卫。
#
# 断言：internal/react 生产代码（排除 _test.go）不再出现对 workspace 路径的
# os.ReadFile|os.WriteFile|os.OpenFile|os.MkdirAll|os.ReadDir|os.Remove|os.Stat 直连；
# 全部 workspace IO 必须经 WorkspaceRuntime.Store()（workspacefs.Store：per-key 锁 + 防穿越），
# 无 runtime 的裸 root 场景经 workspacefs.NewLocalStore。
#
# 白名单（非 workspace 用途，允许 os 直连）：
#   1. internal/react/persistv2/        —— event-sourcing 持久化包整体跳过，M7 统一迁移。
#   2. internal/react/tool_result_render.go —— buildImageDataURL 读取工具产出引用的媒体文件，
#      路径由工具输出给出，可指向 workspace 之外的任意本地文件，Store（root 锚定）不适用。
#
# 说明：os.MkdirTemp（agent_execute.go 创建临时 workspace root）与 os.TempDir
# （tool_output_truncate.go 无 workspace root 时的回退 root）不在断言动词表内——
# 二者发生在 workspace root 尚不存在/不可用的时刻，属 root 供给而非 workspace 内 IO。
#
# 退出码非 0 = 存在违规直连。

set -euo pipefail

cd "$(dirname "$0")/.."

PATTERN='os\.(ReadFile|WriteFile|OpenFile|MkdirAll|ReadDir|Remove|Stat)\('

violations=$(grep -rnE "$PATTERN" internal/react --include='*.go' \
  | grep -v '_test\.go:' \
  | grep -v '^internal/react/persistv2/' \
  | grep -v '^internal/react/tool_result_render\.go:' \
  || true)

if [[ -n "$violations" ]]; then
  echo "[check_workspace_io] 发现未走 workspacefs.Store 的直连 os IO：" >&2
  echo "$violations" >&2
  echo "[check_workspace_io] workspace IO 必须经 WorkspaceRuntime.Store() / workspacefs.NewLocalStore；" >&2
  echo "[check_workspace_io] 确属非 workspace 用途请在本脚本白名单登记并注明理由。" >&2
  exit 1
fi

echo "[check_workspace_io] OK：internal/react 生产代码无 workspace 直连 os IO 残留。"
