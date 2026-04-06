# Go 测试策略

> Type: `general`
> Updated: `2026-04-06`
> Summary: 迁移到 `docs/general` 并统一文档元信息头，测试策略内容未做实质改动。

## 1. 目标

当前测试策略仍然是：

- 自动化优先
- 真实状态机优先
- mock 必须与真实协议一致

“测试通过”在这个仓库里至少意味着两件事：

1. `go test ./...` 全绿
2. 当前用户可见问题在对应层级有覆盖，而不是只改了一个局部函数

## 2. 当前测试层次

### 2.1 纯领域测试

重点模块：

- `internal/core/orchestrator`
- `internal/core/renderer`
- `internal/core/state`
- `internal/core/control`

重点覆盖：

- attach 后默认 thread 选择
- routeMode / dispatchMode 状态机
- queue item 冻结 thread/cwd/override
- staged image 绑定
- local-priority / handoff
- renderer 的文本切分与代码块处理

### 2.2 协议翻译测试

重点模块：

- `internal/adapter/codex`
- `internal/adapter/relayws`

重点覆盖：

- native `turn/start` / `turn/steer` -> canonical event
- remote `prompt.send` -> native `thread/start` / `thread/resume` / `turn/start`
- `initiator` 判定
- `trafficClass=internal_helper` 标注
- helper/internal traffic 不污染正常远端 turn
- websocket hello / command / event / ack 流程

### 2.3 运行时集成测试

重点模块：

- `internal/app/wrapper`
- `internal/app/install`
- `internal/app/daemon`
- `internal/adapter/feishu`

重点覆盖：

- wrapper 与 mock Codex 的桥接
- install 配置写入和 editor patch
- Feishu action 解析
- projector 输出

### 2.4 Harness / e2e

使用：

- `testkit/mockcodex`
- `testkit/mockfeishu`
- `testkit/harness`

重点覆盖：

- attach -> 发文本 -> 收到输出
- 本地 VS Code turn 导致 pause_for_local
- handoff 恢复远端 queue
- helper/internal turn 不进入 Feishu 主渲染
- thread 切换和 queue 冻结

## 3. Mock 约束

### 3.1 `mockcodex`

必须是状态机，而不是静态脚本。

它至少要维护：

- thread 集合
- focused thread
- active turn
- thread cwd
- turn inputs

并且必须支持当前真实链路用到的 native 请求和通知。

### 3.2 `mockfeishu`

必须记录真实副作用，而不是只记录“调用过一次”：

- 文本
- 卡片
- typing reaction
- thumbsdown reaction
- selection prompt

## 4. 代理环境要求

本仓库经常运行在设置了全局 `http_proxy` / `https_proxy` 的机器上。

本地测试和 localhost 检查默认都应该清掉代理环境：

```bash
env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u all_proxy go test ./...
```

## 5. 除了单元测试还应该做什么

对状态机或协议 bug，不应只看单测。

至少还应核对：

- `relayd` 当前 `/v1/status`
- `codex-remote-relayd.log`
- 当前 wrapper instance 是否在线
- 真实 Feishu / VS Code 复现路径是否和测试覆盖的链路一致

## 6. 当前通过标准

当前版本的最小通过标准：

1. `go test ./...` 全绿
2. helper/internal traffic 的标注与 server 侧过滤有测试
3. wrapper / orchestrator / harness 三层至少有一层覆盖用户报告的现象
4. mock 行为不与当前真实协议冲突
