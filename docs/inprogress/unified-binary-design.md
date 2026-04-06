# 单一二进制设计

> Type: `inprogress`
> Updated: `2026-04-06`
> Summary: 迁移到 `docs/inprogress` 并统一文档元信息头，保留统一二进制方案设计。

## 1. 目标

把当前的三个可执行入口：

- `relayd`
- `relay-wrapper`
- `relay-install`

收敛成一个统一二进制，并通过“启动方式 / 启动参数 / 可执行名”决定当前 role。

目标不是改变产品行为，而是：

- 降低 release 产物复杂度
- 降低安装器和脚本对多个二进制文件名的耦合
- 保留 wrapper / daemon / installer 三个运行边界
- 让后续新增 role 或调试入口时，继续沿同一套 launcher 机制扩展

## 2. 现状总结

从当前代码看，三个 `cmd/*` 已经都非常薄：

- [cmd/relayd/main.go](../../cmd/relayd/main.go)
  - 只负责加载统一 `config.json` 中 daemon 关心的键
  - 创建 Feishu gateway
  - 装配 `internal/app/daemon`
- [cmd/relay-wrapper/main.go](../../cmd/relay-wrapper/main.go)
  - 只负责加载 wrapper 配置
  - 装配 `internal/app/wrapper`
- [cmd/relay-install/main.go](../../cmd/relay-install/main.go)
  - 只负责解析 flag
  - 装配 `internal/app/install`

真正的边界已经在 `internal/app/*`：

- `internal/app/daemon`
- `internal/app/wrapper`
- `internal/app/install`

也就是说，当前问题主要不是业务代码耦合，而是“入口和发布形态仍是三份”。

## 3. 设计原则

### 3.1 入口统一，role 不混用

统一二进制不等于把三套运行时混在一起启动。

要求是：

- 进入 `wrapper` role 时，只初始化 wrapper 相关依赖
- 进入 `daemon` role 时，只初始化 daemon 相关依赖
- 进入 `install` role 时，只初始化 installer 相关依赖

launcher 只做：

- 识别 role
- 分发到 role entry
- 统一信号处理
- 统一版本信息

launcher 不做：

- Feishu 逻辑
- app-server 翻译
- 安装流程

### 3.2 wrapper 必须只在 app-server 模式下启动

这是单一二进制设计里最关键的约束。

当前两种 wrapper 接入方式：

- `managed_shim`
  - 扩展 bundle 里的 `codex` 被替换
- `editor_settings`
  - `chatgpt.cliExecutable` 指向自定义路径
  - 可执行名不一定是 `codex`

但这两种方式有一个共同点：

- VS Code 扩展拉起 Codex 时，走的是 `app-server` 子命令
- 本机实际扩展代码里，启动调用形态是：
  - executable = `chatgpt.cliExecutable` 或 bundle 内 `codex`
  - argv[1] = `app-server`
  - 当前版本还会附带 `--analytics-default-enabled`

因此 wrapper 的判定标准不应该是 basename，而应该是：

- 是否进入了 Codex 的 `app-server` 运行模式

这是更稳定的依据，因为：

- `editor_settings` 本来就不保证可执行名
- Windows 上没有必要为了 role 判定额外复制多份别名二进制
- wrapper 只适合代理 JSONL app-server 协议，不适合承接普通 TUI/CLI 模式

## 4. 目标启动模型

目标只发布一个主二进制，建议命名：

- `codex-remote`

release 包里只放：

- `codex-remote` / `codex-remote.exe`

不再把 `codex-remote-wrapper`、`codex-remote-relayd`、`codex-remote-install` 作为独立 release 资产。

## 5. Role 识别规则

建议使用“显式控制子命令 + `app-server` 自动识别”的模型。

### 5.1 显式子命令

统一二进制支持：

```bash
codex-remote daemon
codex-remote install ...
codex-remote wrapper app-server ...
codex-remote version
codex-remote help
```

其中：

- `daemon`
  - 启动 relay 服务
- `install`
  - 运行安装器
- `wrapper`
  - 开发/调试入口
  - 只允许后续参数进入 `app-server ...`
  - 主要给测试和手工验证使用
  
### 5.2 `app-server` 自动进入 wrapper

如果第一个参数是：

- `app-server`

则直接进入 wrapper role，并将后续参数原样传给 wrapper entry。

也就是说，下面这种就是生产主路径：

```bash
codex-remote app-server --analytics-default-enabled
```

这对应当前 VS Code 扩展的真实调用方式。

### 5.3 不再使用 basename 触发

单一二进制设计里，不再依赖 basename 判定 role。

原因：

- `editor_settings` 下可执行文件名本来就不稳定
- `managed_shim` 下虽然路径名通常是 `codex`，但真正稳定的是参数里的 `app-server`
- Windows 不适合围绕 symlink / 多别名文件名来设计主触发逻辑
- basename 只能说明“它叫什么”，不能说明“它现在是不是在跑 app-server 协议”

因此：

- 不要求 `codex`、`relayd`、`relay-install` 这些别名作为长期契约存在
- 即使后续 `managed_shim` 把统一二进制复制到 bundle 的 `codex` 路径，launcher 也仍应主要看参数而不是文件名

### 5.4 不再默认兜底为 wrapper

不能因为“这是统一二进制”就把所有未知参数都送进 wrapper。

原因是 wrapper 不是一个通用的 codex CLI 代理，而是：

- 一个专门桥接 `app-server` JSONL 协议的进程包装器

如果把下面这些调用也误判成 wrapper：

```bash
codex-remote
codex-remote resume --thread abc
codex-remote some-future-cli-command
```

那么 wrapper 会去拉起 `codex.real` 的非 app-server 模式，但自身仍按协议桥去读写 stdio，结果只会让行为更混乱。

因此统一规则应为：

- `daemon/install/version/help` 是 launcher 保留字
- `app-server ...` 自动进入 wrapper
- `wrapper app-server ...` 显式进入 wrapper
- 其他参数模式一律报错并输出 usage，而不是兜底进 wrapper

### 5.5 参数保真规则

单一二进制下，最重要的不是“能识别 role”，而是“不要把 wrapper 原始参数改坏”。

因此要求：

- launcher 只识别：
  - 第一个参数是不是 launcher 保留子命令
  - 或第一个参数是不是 `app-server`
- 一旦判定进入 wrapper role：
  - 不再对剩余参数做二次解析
  - 不新增 wrapper 私有 flag 语法
  - 原始参数顺序必须原样传给 wrapper entry

也就是说，下面两种都必须进入同一条 wrapper 主路径：

```bash
codex-remote app-server --analytics-default-enabled
codex-remote wrapper app-server --analytics-default-enabled
```

launcher 不能尝试理解 `app-server` 后面的原生参数含义，只能透明转发。

## 6. 建议的代码结构

### 6.1 新增统一入口

新增：

```text
cmd/
  codex-remote/
    main.go
```

该入口只负责：

- 调用 launcher
- 注入版本信息

### 6.2 新增 launcher 层

建议新增：

```text
internal/app/launcher/
  launcher.go
  role.go
```

职责：

- 根据 `argv[0] + argv[1:]` 识别 role
- 为 role 创建运行上下文
- 分发到各 role 的 entry 函数
- 提供统一的 `help/version`

### 6.3 各 role 提供 entry 函数

把当前 `cmd/*` 的逻辑下沉为 role entry：

```text
internal/app/daemon/entry.go
internal/app/wrapper/entry.go
internal/app/install/entry.go
```

示意：

- `daemon.RunMain(ctx) error`
- `wrapper.RunMain(ctx, args []string, stdin, stdout, stderr) (int, error)`
- `install.RunMain(args []string, stdin, stdout, stderr) error`

这样 launcher 只管分发，不管 role 内部细节。

### 6.4 现有业务包保持不动

以下包不应因为“单一二进制”而被合并或重命名：

- `internal/app/daemon`
- `internal/app/wrapper`
- `internal/app/install`
- `internal/core/*`
- `internal/adapter/*`

也就是说，这次是“入口与打包层重构”，不是领域层重构。

### 6.5 role entry 契约

建议把 role entry 的边界写得很明确：

```text
launcher
  -> daemon entry
  -> wrapper entry
  -> install entry
```

其中：

- `daemon entry`
  - 负责加载统一 `config.json` 中 daemon 关心的键
  - 负责初始化 gateway / relay server / status API
  - 接收外部传入的 `context.Context`
- `wrapper entry`
  - 负责加载统一 `config.json` 中 wrapper 关心的键
  - 负责捕获并清理自身代理环境
  - 负责拉起 `codex.real`
  - 必须先校验 forwarded args 的第一个参数就是 `app-server`
  - 必须接收原始 `args` 与原始 `stdin/stdout/stderr`
  - 返回 `(exitCode, error)`
- `install entry`
  - 负责解析安装器 flag
  - 负责交互向导
  - 必须接收原始 `stdin/stdout/stderr`

launcher 自己只拥有两类能力：

- role 判定
- 顶层信号与退出码管理

launcher 不应：

- 提前加载任意 role 配置
- 提前修改代理环境
- 提前占用 stdin
- 在进入 wrapper 前改写 stdout/stderr

否则会直接破坏 app-server / install wizard 的行为。

### 6.6 顶层信号与退出码

建议统一约定：

- 信号监听只在 launcher 层做一次
- launcher 创建根 `context.Context`
- 各 role entry 不再重复创建自己的 `signal.NotifyContext`

这样做的原因是：

- 避免每个入口各自复制一套退出处理
- 避免未来再加 role 时把生命周期逻辑继续分散
- 让测试可以直接调用 role entry，而不是依赖真实 OS signal

退出语义建议为：

- `daemon/install`
  - `error == nil` -> 进程返回 0
  - `error != nil` -> 进程返回 1
- `wrapper`
  - 以 role 自身返回的 `exitCode` 为准
  - `error != nil` 时由 launcher 决定是否打印并返回非零

wrapper 需要保留这个特殊语义，因为它本质上仍然是 `codex.real` 的进程代理。

## 7. 配置设计

当前运行时已切到统一配置文件：

- `config.json`

其中同时包含：

- wrapper role 需要的键
- daemon role 需要的键

两个 loader 进入 role 后各取所需，不再默认维护两份分裂的 env 文件。

因此 role 进入后仍按现在的方式读取：

- wrapper role -> `LoadWrapperConfig`
- daemon role -> `LoadServicesConfig`
- install role -> 自己的 flags 和平台默认值

关键要求是：

- launcher 在 role 识别之前不要提前读取任意配置
- 只有进入对应 role 之后，才允许读取该 role 的配置文件

这样可以保证：

- `codex-remote app-server ...` 不会意外读取 daemon 配置
- `codex-remote daemon` 不会因为 wrapper 配置错误而无法启动
- install role 可以在“尚未安装、尚无配置文件”时正常运行

## 8. 安装器与集成方式的变化

### 8.1 安装器只安装一个 binary

当前 install service 的 `Options` 是：

- `WrapperBinary`
- `RelaydBinary`

目标应改为一个统一字段，例如：

- `BinaryPath`

然后安装器只复制一次统一二进制。

### 8.2 editor settings 集成

当前 `editor_settings` 会把 `chatgpt.cliExecutable` 指向 wrapper binary。

单一二进制后，直接指向：

- `codex-remote`

当前本机扩展代码的真实行为是：

- 读取 `chatgpt.cliExecutable`
- 以 `app-server` 作为第一个参数启动 Codex

因此单一二进制后，主路径应为：

- `codex-remote app-server --analytics-default-enabled`

也就是说，`editor_settings` 能工作不是因为可执行名，而是因为参数里有 `app-server`。

### 8.3 managed shim 集成

当前 `managed_shim` 会把 bundle 中的 `codex` 替换成 wrapper binary。

单一二进制后，仍可把同一个 binary 复制到 bundle 的 `codex` 路径。

这里也不应依赖 basename。

正确理解应该是：

- 扩展依然会启动 bundle 里的 `codex`
- 但其 argv 仍是 `app-server ...`
- 所以同一个统一二进制会因为第一个参数是 `app-server` 而进入 wrapper

这也意味着：

- 不需要为了 role 判定额外维护 symlink 方案
- Windows 直接复制统一二进制到 bundle `codex(.exe)` 路径即可

### 8.4 install-state.json

建议从：

- `InstalledWrapperBinary`
- `InstalledRelaydBinary`

收敛成：

- `InstalledBinary`

若迁移期要兼容旧状态文件，可以：

- 新增 `InstalledBinary`
- 保留旧字段一两个版本
- 读取时优先新字段，回退旧字段

## 9. 脚本与发布变化

### 9.1 `install.sh`

应改为：

- 构建一个 binary
- `start` 时执行：`codex-remote daemon`
- `bootstrap` 时执行：`codex-remote install ...`

### 9.2 `install-release.sh`

当前已经进一步收敛为：

- release 包下载后不再执行包内 `setup.sh`
- 直接执行：

```bash
codex-remote install -bootstrap-only -start-daemon
```

- 由 WebSetup / Admin UI 接管后续 Feishu 与 VS Code 配置

### 9.3 `scripts/release/build-artifacts.sh`

目标是：

- 每个平台只构建一个 `codex-remote`
- release 包内附带最终用户需要的文档和 `deploy/`
- 在线安装脚本作为独立 release 资产单独发布
- 不再生成三份独立的 exe / elf

## 10. 兼容策略

建议分两阶段迁移。

### 阶段 A：引入统一 launcher，但保留旧 `cmd/*`

做法：

- 新增 `cmd/codex-remote`
- 新增 launcher
- 三个旧 `cmd/*` 改成薄 shim，内部调用同一套 launcher / role entry

收益：

- 先把运行逻辑统一
- 不立即打破现有脚本和本地习惯

### 阶段 B：安装、脚本、release 全切换到单一 binary

做法：

- release 只产出 `codex-remote`
- release script 直接驱动单一 binary 的 bootstrap 路径
- `setup.sh` / `setup.ps1` 降级为源码仓库 helper
- `install.sh` 保留为仓库联调 helper
- 删除旧 binary 资产

等这一阶段稳定后，再考虑是否删除旧 `cmd/*` 源码入口。

## 11. 测试策略

单一二进制改造最容易坏在“入口判定”，不是业务逻辑。

因此测试重点应放在 launcher。

### 11.1 launcher 单测

覆盖以下矩阵：

- `codex-remote`, args = `app-server --analytics-default-enabled` -> wrapper
- `codex-remote`, args = `wrapper app-server --analytics-default-enabled` -> wrapper
- `codex-remote`, args = `daemon` -> daemon
- `codex-remote`, args = `install ...` -> install
- `codex-remote`, args = `version` -> version
- `codex-remote`, args = empty -> usage / non-zero
- `codex-remote`, args = `resume ...` -> usage / non-zero
- `codex-remote`, args = `wrapper resume ...` -> usage / non-zero

### 11.2 install 测试

覆盖：

- 安装器只复制一个 binary
- editor settings 指向统一 binary
- managed shim 复制统一 binary 到 `codex` 路径
- `install-state.json` 正确记录统一 binary

### 11.3 smoke test

至少验证：

1. `codex-remote daemon`
2. `codex-remote install ...`
3. `codex-remote app-server ...` 被当成 wrapper
4. 被复制到 bundle `codex` 路径后，`codex app-server ...` 仍正常
5. `codex-remote resume ...` 不会误进 wrapper

## 12. 以后开发的约束

后续如果再加 role 或入口，遵守以下规则：

- 不新增新的 `cmd/<role>` 作为长期入口
- 先在 launcher 里注册 role，再在 `internal/app/<role>` 放 entry
- 业务逻辑继续放在 `internal/app` / `internal/core` / `internal/adapter`
- launcher 只能做分发，不能承载产品逻辑

换句话说：

- “单一二进制”是发布与启动模型
- 不是把内部模块重新揉成一个包

## 13. 推荐实现顺序

建议后续按这个顺序落地：

1. 新增 launcher 和 `cmd/codex-remote`
2. 把现有三个 `main.go` 下沉为 role entry
3. 为 launcher 补 role 识别测试
4. 修改安装器数据模型，从双 binary 收敛成单 binary
5. 修改 `install.sh` / `setup.sh` / release 脚本，并把产品配置入口切到 WebSetup
6. 最后再移除旧二进制发布形态

这样风险最低，也最容易定位问题。
