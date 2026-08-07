# Codex Remote Feishu 1.9.1 更新内容

## 这次更新带来的主要变化

### 模型配置更灵活了

- Codex / Claude Profile 可以单独配置子代理模型，主模型和子代理模型不再强制共用一套配置。
- Profile 支持可选的角色提示词（instruction），可以给不同 Profile 定义不同的系统提示。
- Codex Profile 新增小米 MiMo 模型支持：baseURL 填官方端点（按量或 Token Plan）或自己的中转地址都能用，模型下拉菜单会自动带上 MiMo 模型。

### 出问题的时候更好排查了

- Codex 能力探测失败时分类更清楚，不再把"本机原生配置探测失败"误报成 API Profile 的问题。
- 探测失败的具体阶段会透出到错误详情里，排查时能直接看到卡在哪一步。

### 群聊和会话恢复更稳

- 群聊按需恢复失败时，不会跨消息反复弹同一条错误。
- 会话还没被接管、还在选目标时发的消息不会再悄悄丢失。
- 自动恢复会话连接成功后，排队中的待处理消息能正确接上继续处理。
- headless 启动时对所有情况都校验工作目录，不只恢复场景。

### 安装和升级更稳

- 在线安装脚本会显示下载进度，装大包时不再像卡住。
- 升级时旧的 version-scoped 安装入口会自动迁移到统一目录，不会残留旧路径。
- release 资产里不再附带在线安装脚本，避免脚本与版本不同步。

## 更细的更新列表

### 功能

- Codex / Claude Profile 支持子代理模型配置（[#822](https://github.com/kxn/codex-remote-feishu/commit/5414ae716d9eab47a36959e1b7ee324373ea7ad0)）
- Codex / Claude Profile 支持可选 instruction（角色提示词）（[#823](https://github.com/kxn/codex-remote-feishu/commit/e7c1048c0ff3e88a72f55b4e0692cf721298735c)）
- Codex Profile 支持小米 MiMo 模型目录：官方端点与中转地址均可识别，模型下拉自动带出 MiMo 模型（[9308c3c7](https://github.com/kxn/codex-remote-feishu/commit/9308c3c7b2fc18b1dbe535d68ba05123254d356c)）
- 在线安装脚本增加下载进度提示（[b0f78b96](https://github.com/kxn/codex-remote-feishu/commit/b0f78b961317e3e9ea74e8ad15a3b896b44d409f)）

### 修复

- 拆分 Codex capability 探测失败分类，解除 native probe 对 API Profile 的误报；probe stage 透出到错误详情（[#832](https://github.com/kxn/codex-remote-feishu/commit/71ed8dd4e45a24755ba771b96d77f1fa10000af2), [2c5daf4b](https://github.com/kxn/codex-remote-feishu/commit/2c5daf4b912050ced5e58c8bb236e60a80874997)）
- 群聊 on-demand 恢复 terminal 失败跨消息去重（[#833](https://github.com/kxn/codex-remote-feishu/commit/ddb00ed7188e20c92ba0fea2a3dadd759a34cc77)）
- 修复 unbound / picker 状态下消息丢失（[b4fc9170](https://github.com/kxn/codex-remote-feishu/commit/b4fc917070ca923c08f6f06a13601e83f4ea782c), [c7f4149e](https://github.com/kxn/codex-remote-feishu/commit/c7f4149eac2993ccb8cdd7a698c11276cd437ac2)）
- auto-restore 成功连接后正确消费 PendingHeadless（[5dd889ab](https://github.com/kxn/codex-remote-feishu/commit/5dd889abb3f452b65d16e51353dc30c63a4f644a)）
- headless 启动对所有 start 校验工作目录，不只 auto-restore（[50041650](https://github.com/kxn/codex-remote-feishu/commit/500416503c0ebdb696c4f8a257b42c972818802b)）
- 升级时迁移 legacy version-scoped stable 入口到 canonical bin dir（[18d23060](https://github.com/kxn/codex-remote-feishu/commit/18d23060526b56b085e1b1f8c0f50cca549dbb33)）
- release 资产移除在线安装脚本（[c9c28a06](https://github.com/kxn/codex-remote-feishu/commit/c9c28a0697851470e95a30b09816d24c99a65f8a)）
- feishu facts 统一 bot info + scopes + open_id 缓存与刷新（[51c4acc5](https://github.com/kxn/codex-remote-feishu/commit/51c4acc5dc1f5259696e80c582b9b13ffe5cbce9)）
- 修复 shim Windows 路径转义与 macOS 测试临时目录符号链接差异（[c8ad54f8](https://github.com/kxn/codex-remote-feishu/commit/c8ad54f897e727e2c01620831469f18b0917b0d2), [a1c36a1b](https://github.com/kxn/codex-remote-feishu/commit/a1c36a1b777d7662484b4336cb6dc9f2784b2e9c)）

### 文档

- 子代理模型配置设计（[#822](https://github.com/kxn/codex-remote-feishu/commit/24818642db06d4f03aacff0693347489d78a47ea)）、instruction 配置设计（[#823](https://github.com/kxn/codex-remote-feishu/commit/648e502774693ed06f37f5eb907d7340ee290a88)）、probe 失败分类同步（[#832](https://github.com/kxn/codex-remote-feishu/commit/faa32953bc3f57f2227fb7ced3d04ea43487f234)）
- 开发工作流约定：AGENTS 精简、issue 工作流风险分级、gh 使用规则、输出防刷屏阈值（[112e45b9](https://github.com/kxn/codex-remote-feishu/commit/112e45b98f940c3bf40b133eb9afeb0038fb7192), [992e61ef](https://github.com/kxn/codex-remote-feishu/commit/992e61efc49af93bd3978947a7ca8b5ec3b2d88c), [62dc274e](https://github.com/kxn/codex-remote-feishu/commit/62dc274e5b7b348f7a1539194d2ea5101bb4ffea), [3c988c1f](https://github.com/kxn/codex-remote-feishu/commit/3c988c1f2eff91e958fa68de7d9e83621895c5db)）
- 清理指向已删除符号的文档引用（[0ace8b88](https://github.com/kxn/codex-remote-feishu/commit/0ace8b8880208e739a1880b04e58e4cace8465a5)）
- CI 新增跨平台路径检查脚本，预防 macOS / Windows 路径陷阱（[d56faa96](https://github.com/kxn/codex-remote-feishu/commit/d56faa96264173be114c21c093fd4a377fdec76c)）
