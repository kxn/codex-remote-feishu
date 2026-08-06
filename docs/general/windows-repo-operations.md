# Windows 仓库操作规范（WSL 禁用与 PowerShell/gh 使用）

> Type: `general`
> Updated: `2026-08-06`
> Summary: Windows 上进行仓库操作时的 shell 与 gh 规范：禁用 WSL，统一使用 Git Bash 与 Windows git，记录 PowerShell 5.1 下 gh 的已验证姿势。

## WSL 禁用

- 禁止在仓库操作中使用任何 WSL 命令：`wsl` / `wsl.exe` / `C:\Windows\system32\bash.exe`（WSL bash）。
- 原因（实测踩坑，2026-08-05）：WSL 内的 git 与 Windows git 对行尾（CRLF/LF）、配置、路径的视角不一致。同一干净工作区，WSL git 会误报 200+ 个文件“每行都变”，并打印 `wsl: Processing /etc/fstab with mount -a failed.` 噪音，导致 safe-push / status 检查不可信。
- 需要 bash 时统一用 Git Bash：`C:\Program Files\Git\bin\bash.exe`（或 `C:\Program Files\Git\usr\bin\bash.exe`）。它使用 Windows git，视角一致。
- PowerShell 中调用示例：
  ```powershell
  & "C:\Program Files\Git\bin\bash.exe" -c 'cd /e/temp/codex-remote-feishu && ./safe-push.sh'
  ```
- 执行前先确认 bash 来源：`Get-Command bash | Select-Object Source`。若指向 `C:\Windows\system32\bash.exe` 就是 WSL，必须改用 Git Bash 完整路径。
- 不要用裸 `bash` / `git` 让系统解析到 WSL；仓库操作一律走 Windows git（`C:\Program Files\Git\cmd\git.exe`）。
- WSL 仅允许用于与仓库无关、且用户明确要求 WSL 的操作。

## PowerShell 5.1 与 gh

PowerShell 5.1 与 Unix shell 的引号 / 管道 / 编码语义不同，直接照抄 Unix 习惯的 gh 命令会反复翻车（实测踩坑记录，2026-08-05）。以下为验证过的正确姿势：

### 命令分隔

- PowerShell 5.1 不支持 `&&` / `||`，报错 `The token '&&' is not a valid statement separator`。多条命令用 `;` 分隔，或分多次工具调用。

### 输出与 jq

- 能不用 `--jq` 就不用：默认表格输出（`gh run list` / `gh issue list`）或 `--json` + PowerShell `ConvertFrom-Json` 最稳。
- 必须用 `--jq` 时：
  - 表达式内不要使用 `\"` 转义（如 `'.[] | "\(.number): \(.title)"'`）——PowerShell 会把 `\"` 原样传给 gh，报 `unknown argument`。
  - 用 `@tsv` / `@csv` 平铺输出：`--jq '.[] | [.number, .title] | @tsv'`。
  - 不要在表达式里用切片 `[0:8]`（实测报 `function not defined: a/0`）；需要截断时在 PowerShell 侧处理。
- PowerShell 侧解析：`$t = gh api <url> | ConvertFrom-Json`，再用 `Where-Object` / `ForEach-Object` 过滤，避免复杂 jq。

### 通过 API 发 JSON（评论 / 创建资源）

- 不要 `$json | gh api --input -`：PS 5.1 管道编码 + `ConvertTo-Json` 对象包装会报 `Problems parsing JSON`。
- 正确姿势（无 BOM UTF-8 临时文件，从文件读）：
  ```powershell
  $body = [string][System.IO.File]::ReadAllText("$env:TEMP\msg.md")
  $json = @{ body = $body } | ConvertTo-Json
  [System.IO.File]::WriteAllText("$env:TEMP\payload.json", $json, (New-Object System.Text.UTF8Encoding($false)))
  gh api -X POST repos/o/r/issues/1/comments --input "$env:TEMP\payload.json"
  ```
- `Get-Content -Raw` 在 PS 5.1 返回的是对象而非纯字符串，`ConvertTo-Json` 会包一层 `{ "value": ... }`，务必用 `[string]` 强转或 `[System.IO.File]::ReadAllText`。

### 版本差异

- 先 `gh <cmd> --help` 确认本机版本支持的 flags，不要凭记忆。例如本机 `gh issue close` 只有 `--comment`，没有 `--comment-file`；长评论走上面的 API 方式（先发评论再关单）。

### 其他

- `gh run watch <run-id> --exit-status --interval 20` 可阻塞等待 CI 结果。
- 引用 jq 输出时避免含空格的表达式；不确定时先跑 `gh ... --json fields` 看原始 JSON。
