# Non-Linux User Autostart Research

> Type: `draft`
> Updated: `2026-04-09`
> Summary: 调研 macOS / Windows 当前用户自动启动机制，并给出适合 codex-remote 的推荐方向。

## 1. 背景

当前仓库已经支持 Linux 下的 `systemd --user` 守护进程管理，但 macOS 和 Windows 仍只有 `detached` 模式。

本次调研只回答一个更窄的问题：

- 对于“以当前用户身份启动 `codex-remote daemon`”这件事，macOS / Windows 各自有哪些平台原生机制？
- 哪个机制最接近当前 Linux `systemd_user` 的使用目标？
- 哪些机制虽然能跑，但不适合作为默认支持路径？

## 2. 结论摘要

推荐把非 Linux 平台的一期目标收敛为：`登录后自动启动`，而不是“开机但未登录前就以当前用户身份运行”。

- macOS:
  - 推荐使用 `LaunchAgent`
  - 安装位置为 `~/Library/LaunchAgents`
  - 语义是“当前用户登录后自动启动”
- Windows:
  - 推荐使用 `Task Scheduler` 的 `LogonTrigger`
  - 语义是“指定用户登录后自动启动”
- 不推荐作为主方案的路径:
  - macOS `Login Items`
  - Windows `Run` / `RunOnce` registry key
- 暂不纳入一期的路径:
  - macOS “未登录前仍以当前用户身份运行”
  - Windows “未登录前仍以当前用户身份运行”

## 3. 为什么这样收敛

### 3.1 macOS

Apple 当前仍明确建议通过 `launchd` 管理 daemon / agent，并且说明其他启动机制可能被移除。Apple 也明确区分了：

- `~/Library/LaunchAgents`: 只作用于当前登录用户
- `/Library/LaunchDaemons`: 系统级 daemon

Apple 的后台进程文档进一步说明：

- `Launch Daemon` 运行在 system context
- `Launch Agent` 运行在当前登录用户的 user context
- daemon 可在没有用户登录时继续运行
- agent 则属于当前用户会话

这意味着：

- 如果目标是“当前用户身份 + 无需 root + 与用户配置目录天然一致”，`LaunchAgent` 才是最接近 Linux `systemd --user` 的模型
- 如果要求“还没登录就启动”，那已经偏向 system daemon 语义，不再是单纯的“当前用户服务”

### 3.2 Windows

Windows 原生可选项里，真正接近“可管理的后台任务”的是 `Task Scheduler`，而不是 `Run` key。

官方文档说明：

- `Run` key 只保证“用户登录时运行”
- command line 长度限制为 260 字符
- 多个项目执行顺序不确定
- 系统不保证会立刻执行，甚至可能为了前台体验而延迟

相比之下，`Task Scheduler` 可以明确表达触发条件和运行身份：

- `LogonTrigger`: 用户登录时触发
- `TASK_LOGON_INTERACTIVE_TOKEN`: 用户必须已经登录，任务运行在现有交互会话中

而“未登录前仍以某个普通用户身份运行”这条路在 Windows 上要么需要管理员创建 `BootTrigger`，要么要用：

- `TASK_LOGON_PASSWORD`: 注册时存储密码
- `TASK_LOGON_S4U`: 不存密码，但没有 network / encrypted files 访问能力

这两条都不适合作为当前项目默认安装路径：

- 存储用户密码会明显抬高安全和 UX 成本
- `S4U` 无法访问网络，和 `codex-remote daemon` 的实际使用场景冲突

## 4. 对当前仓库结构的影响

现有安装层已经有 `ServiceManager` 抽象，但实现和持久化仍明显偏 Linux：

- `internal/app/install/service_manager.go`
- `internal/app/install/service_entry.go`
- `internal/app/install/linux_service.go`
- `internal/app/install/install_metadata.go`

目前的结构问题不是“不能做”，而是“需要先泛化”：

- `ServiceManager` 目前只有 `detached` 和 `systemd_user`
- `InstallState.ServiceUnitPath` 明显是 systemd 专属字段
- `service status/start/stop/restart` 当前只实现了 systemd 分支

如果继续往下做，建议先把服务元数据改成平台无关的表达，例如：

- `ServiceDefinitionPath`
- `ServiceID`
- 或者一层 `ServiceMetadata` 结构，按 manager 存特定字段

不要继续把 macOS / Windows 信息硬塞进 `ServiceUnitPath` 这种 Linux 专名里。

## 5. 平台建议

### 5.1 macOS 一期建议

推荐新增 `launchd_user` 类似的 service manager，语义为“当前用户登录后自动启动 daemon”。

建议能力范围：

- `service install-user`
  - 写入 `~/Library/LaunchAgents/<label>.plist`
- `service enable`
  - 通过 `launchctl bootstrap` / `enable` 加载
- `service disable`
  - 通过 `launchctl bootout` / `disable` 卸载
- `service start`
  - 通过 `launchctl kickstart` 立即拉起
- `service stop`
  - 通过 `launchctl bootout` 或等价操作停止
- `service status`
  - 通过 `launchctl print` 或等价查询获取状态

不建议一期支持：

- system-wide `LaunchDaemon`
- “未登录前以当前用户身份运行”
- GUI `Login Items`

### 5.2 Windows 一期建议

推荐新增 `task_scheduler_logon` 类似的 service manager，语义为“当前用户登录后自动启动 daemon”。

建议能力范围：

- `service install-user`
  - 注册当前用户的计划任务
- `service enable`
  - 启用任务
- `service disable`
  - 禁用任务
- `service start`
  - 立即触发任务运行
- `service stop`
  - 结束当前 daemon 进程，而不是依赖 Task Scheduler 自己的抽象停止
- `service status`
  - 查询任务是否已注册、启用以及最近运行结果

推荐关键约束：

- 使用 `LogonTrigger`
- 使用当前用户身份
- 选择交互式登录 token 语义，而不是密码持久化语义

不建议一期支持：

- `Run` / `RunOnce` 作为正式 service manager
- `BootTrigger`
- 需要保存用户密码的任务注册路径

## 6. 建议的 issue 拆分

本调研完成后，后续实现建议拆成两个独立 issue：

1. macOS `LaunchAgent` service manager
2. Windows `Task Scheduler` logon service manager

如果未来产品真的要求“用户未登录前也要自动启动”，建议另开单独 issue 讨论安全模型，而不是和上述两项混做。

## 7. 参考资料

- Apple Support: Script management with launchd in Terminal on Mac
  - https://support.apple.com/en-uz/guide/terminal/apdc6c1077b-5d5d-4d35-9c19-60f2397b2369/2.15/mac/26
- Apple Developer Archive: Daemons and Services Programming Guide
  - https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/DesigningDaemons.html
- Microsoft Learn: Principal.LogonType property
  - https://learn.microsoft.com/en-us/windows/win32/taskschd/principal-logontype
- Microsoft Learn: Run and RunOnce Registry Keys
  - https://learn.microsoft.com/en-us/windows/win32/setupapi/run-and-runonce-registry-keys
- Microsoft Learn: ILogonTrigger interface
  - https://learn.microsoft.com/en-us/windows/win32/api/taskschd/nn-taskschd-ilogontrigger
- Microsoft Learn: IBootTrigger interface
  - https://learn.microsoft.com/en-us/windows/win32/api/taskschd/nn-taskschd-iboottrigger
