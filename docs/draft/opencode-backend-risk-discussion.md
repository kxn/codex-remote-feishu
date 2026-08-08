# OpenCode Backend 风险与降级讨论

> Type: `draft`
> Updated: `2026-08-09`
> Summary: 按 Claude 现有产品基线收敛 OpenCode backend 的剩余硬门槛和实现前提。

## 1. 结论

OpenCode 可以进入完整设计。对照当前 Claude backend 后，大部分差异都不是需要产品拍板的风险，而是 backend adapter 的承接细节：能投影就投影，不能投影就按现有 Claude 式产品壳自然退化，必要时写 debug trace，不把内部 carrier 差异暴露给用户。

当前唯一设计前硬门槛是 profile/auth：

- 如果 OpenCode profile 明确配置 API key / base URL / model，这个实例必须稳定使用该 API profile；
- 系统上原本存在的 OpenCode OAuth 登录态不得覆盖或污染这个 API profile；
- 我们不要求、也不支持在后台替用户首填或管理多个 OAuth profile；
- 系统 OAuth 只作为“继承系统现状”的默认/非隔离模式存在；如果它会干扰 API profile，则第一版直接禁用 OAuth/inherit 路径也可以接受。

推荐默认策略：

- 第一版按 `API key + 本地 profile overlay + ACP runtime adapter + backend capability profile` 做。
- 不承诺 OAuth profile 隔离、OS/container sandbox、历史会话删除、未知 slash command 透传。
- Plan、命令、usage、error 允许像 Claude 一样做 adapter synthesis 或 backend profile 降级；这些优先是实现细节，不默认变成用户可见“不支持”提示。
- 用户可见产品面只保留现有 Claude/Codex 类似入口；底层差异默认写 debug trace 或日志。

## 2. Claude 基线校准

| 能力面 | Claude 现状 | 对 OpenCode 的校准结论 |
| --- | --- | --- |
| Profile / auth | 自定义 profile 继承系统环境，只移除并覆盖 `ANTHROPIC_*`、subagent model、reasoning、追加 instruction 等有限 env；默认 profile 是 inherit。没有通用配置目录/OAuth 完全隔离抽象。 | OpenCode 不需要通用 loader 抽象；只需要证明 API profile overlay 能压过系统 OAuth，不被系统登录态影响。 |
| 权限 / sandbox | Claude 把 native `default`、`acceptEdits`、`plan`、`bypassPermissions` 投影到 access/plan；不是 OS/container sandbox。 | OpenCode 不补齐 Codex sandbox，也不新增用户可见风险；按现有权限/访问语义承接，底层差异只进 debug trace。 |
| Plan | Claude 是拼出来的产品语义：`ExitPlanMode` 走确认卡，`TodoWrite` 有结构化输入时投影计划更新卡，普通计划正文仍可作为普通 assistant/plan 文本承接。它没有向用户暴露内部 carrier 差异。 | OpenCode 比照 Claude：能从 ACP/tool/text 稳定承接就承接；不能承接时自然退化为普通内容或 backend plan mode，不需要专门提示用户内部承接方式。 |
| 命令兼容 | Claude command profile 已隐藏/拒绝 `/compact`、`/review`、`/patch`、`/auto-continue`、`/auto-whip` 等多项 Codex 命令；部分 `/new`、workspace list、steer all 是 approximation。 | OpenCode 也应做 backend command profile。未知或未验证命令发送前拒绝，不作为阻塞项。 |
| 历史会话 | Claude catalog 从本地 session store/list 读取，只有 recent/list/thread lookup 语义，没有在 catalog 层提供删除历史。 | OpenCode `session/close` 按 detach/stop runtime 承接即可；不额外产品化 close/delete 差异。 |
| Usage | Claude 从 result `usage` 合成 last/total token usage，并用 `modelUsage.contextWindow` 补 context window；这是投影，不是统一原生账单语义。 | OpenCode 也按现有 usage 事件尽量投影。只有当产品要展示 ACP context meter 时才需要单独讨论。 |
| Error | Claude 当前失败主要归一到 `claude_turn_failed`，details 带原始 errors；不是细粒度完整 taxonomy。 | OpenCode 不必做完整 taxonomy；只需把 auth/session/MCP/model 这类影响用户下一步操作的错误提前诊断或归一化。 |

## 3. 设计前硬门槛

### 3.1 API Profile Overlay 必须压过系统 OAuth

必须证明：

- 系统已有 OpenCode OAuth 登录态时，启动一个显式 API key profile 实例，实际 provider/model/auth 走 profile overlay，而不是系统 OAuth。
- 两个不同 API key profile 可以并发启动，互不污染，也不写回系统 OpenCode 配置。
- 缺少 API key 或 overlay 无效时，启动前失败或在首轮前给内部诊断；不能悄悄 fallback 到系统 OAuth。

推荐方案：

- 第一版只把 API-key profile 定义为 fully supported profile。
- 默认 profile 可以继承系统 OpenCode 现状，包括用户自己已有的 OAuth；我们不负责配置、刷新或隔离这个 OAuth。
- 自定义 API profile 启动时使用 `OPENCODE_CONFIG_CONTENT` + `OPENCODE_AUTH_CONTENT`，必要时配合独立 XDG root，确保系统 OAuth 不参与该实例。
- 如果实测发现 API overlay 仍会受系统 OAuth 影响，则第一版禁止 OpenCode OAuth/inherit 模式，只允许 API profile。

完整设计前必须补的测试：

- 系统 OAuth 存在 + API profile overlay：请求捕获证明 authorization/baseURL/model 来自 profile。
- 系统 OAuth 存在 + 两个 API profiles 并发：请求捕获证明互不串 key/model/baseURL。
- API profile 启动后扫描系统 OpenCode config/auth/data 文件：证明没有写回或污染系统配置。

如果这组测试通过，就可以进入完整设计；如果失败，则设计结论改成“OpenCode 第一版只支持系统 inherit 或只支持 API profile 二选一”，不能同时承诺二者。

## 4. 从原风险清单降级的项

- `/review`、`/compact`、`/patch`、`/auto-continue`、`/auto-whip`：按 Claude command profile 的先例，属于 backend capability/command profile 问题，不是 OpenCode 接入 blocker。
- Plan snapshot / Plan UI 支持：按 Claude 先例属于 adapter 投影和现有卡片承接问题，不是产品风险。
- persistent delete：Claude catalog 也没有统一删除语义，OpenCode 只需避免把 close 说成 delete。
- 完整 error taxonomy：Claude 也没有完整细粒度分类；OpenCode 第一版只要求关键可行动错误归一化。
- Codex 强 sandbox：Claude 未对齐 Codex 强 sandbox，OpenCode 也不需要补齐；这不是 OpenCode 完整设计前的产品决策。
- MCP OAuth：第一版不支持我们侧首填或隔离 OAuth；系统已有 OAuth 只属于默认 inherit 模式，API profile 必须绕开它。
- Usage/context meter：第一版不新增用户可见 context meter，只投影现有 token usage；差异进 debug trace。

## 5. 实现顺序建议

1. 先做 API profile overlay 黑盒验证：系统 OAuth 存在时，API profile 仍能稳定覆盖 auth/baseURL/model。
2. 再做 ACP runtime adapter：turn buffer、load hydration、tool/permission/usage/error mapping。
3. 再做 command profile：可见/可发/近似/拒绝与诊断文案。
4. 再按 Claude 产品语义接 Plan：mode/确认/普通计划文本/可结构化 todo 各走现有承接面，不新增内部 carrier 提示。
5. 最后补 profile compiler、secret redaction 与调试日志；OAuth 管理不进入第一版。
