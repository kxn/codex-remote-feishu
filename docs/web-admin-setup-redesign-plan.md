# Web Admin & Setup 界面重构方案

## 概述

对 `web/` 目录下的 Admin（管理页面）和 Setup（设置向导）界面进行视觉重设计、文案重写和移动端适配优化。**只改样式和文案，不动功能逻辑。**

---

## 一、现状问题清单

### 1.1 文案问题

当前界面上的文案来自两个来源：人工手写（错误提示、操作反馈等，质量较好）和 GPT 生成（section description、组件引导语等，问题较多）。

**GPT 生成文案的典型问题：**
- 用文档语言解释 UI 行为，而非指导用户操作（如 "进入后自动检查服务与运行条件"、"页面会自动轮询并继续下一步"）
- 冗长、包含无信息量的功能分类描述（如 "管理机器人、系统集成与本地存储"）
- 中英文混杂不自然（如 "创建一套新的 Claude 连接配置"、"创建一套新的 Codex Provider"）
- 带有设计意图的自言自语（如 "使用这台机器当前可用的 Claude 设置"）

**需要修改的文案清单：**

| 文件 | 行号附近 | 当前文案 | 问题 | 建议改为 |
|------|---------|---------|------|---------|
| AdminRoute | 901 | `管理机器人、系统集成与本地存储。` | GPT 分类描述 | `飞书机器人、系统集成与本地存储` |
| AdminRoute | 949 | `查看所有机器人并处理需要关注的状态。` | 过度描述 | `已接入的飞书应用` |
| AdminRoute | 630 | `选择扫码创建或手动输入，连接验证通过后会自动加入机器人列表。` | 文档化 UI 行为 | `扫码或手动输入接入飞书应用` |
| AdminRoute | 997 | `统一管理自动运行设置与 VS Code 集成。` | GPT 拼凑 | `自动运行与 VS Code 集成` |
| AdminRoute | 1060 | `查看占用并按需清理旧文件。` | 过于口语化 | `预览文件、图片暂存与日志清理` |
| AdminRoute | 799 | `机器人状态与测试入口。` | 无信息量 | `连接状态、权限与测试` |
| AdminRoute | 783 | `验证并保存` | 尚可 | `连接并验证` |
| AdminRoute | 805 | `连接状态` | 尚可 | `连接` |
| AdminRoute | 488 | `启用自动运行` | 尚可 | 不变 |
| AdminRoute | 656 | `请使用飞书扫码完成创建，页面会自动轮询并继续下一步。` | 解释技术实现 | `使用飞书扫描二维码，页面将自动完成后续操作` |
| SetupRoute | 705 | `进入后自动检查服务与运行条件。` | 文档化行为 | `确认服务与运行条件正常` |
| SetupRoute | 709 | `环境检查通过，正在进入飞书连接...` | 冗余 | `环境正常` |
| SetupRoute | 752 | `选择扫码创建或手动输入，连接验证通过后会自动进入下一步。` | 同 Admin | `扫码或手动输入接入飞书应用` |
| SetupRoute | 784 | `请使用飞书扫码完成创建，页面会自动轮询并继续下一步。` | 同 Admin | `使用飞书扫描二维码，页面将自动完成后续操作` |
| SetupRoute | 1575 | `可随时回看已完成步骤。` | 步骤状态已表达 | 删除，改为空或 `共 9 步` |
| SetupRoute | 934 | `进入页面后自动检查当前权限。` | 文档化行为 | `确认飞书应用权限配置正确` |
| SetupRoute | 946 | `权限已完整，系统会自动进入下一步。` | 混淆 | `权限配置正确` |
| SetupRoute | 1029 | `暂时没有拿到最新结果，请重新检查。` | 冗余 | `检查失败，请重试` |
| SetupRoute | 1119 | `进入本页后，机器人会自动发出事件订阅测试提示。` | 文档化行为 | `机器人已尝试发送事件订阅测试` |
| SetupRoute | 1177 | `进入本页后，机器人会自动发出回调测试卡片。` | 文档化行为 | `机器人已尝试发送回调测试卡片` |
| SetupRoute | 1235 | `请在飞书后台完成菜单配置后继续下一步。` | 尚可 | `在飞书后台配置机器人菜单` |
| SetupRoute | 1273 | `正在检查当前系统是否支持自动启动。` | 冗余 | `检测系统是否支持自动启动` |
| SetupRoute | 1348 | `请选择是否在登录后自动运行。` | 尚可 | 不变 |
| SetupRoute | 1382 | `是否在这台机器上使用 VS Code + Codex。` | 自言自语 | `VS Code 集成设置` |
| SetupRoute | 1434 | `如果你需要在这台机器上使用 VS Code，请完成集成。` | 废话 | 删除，标题已表述 |
| SetupRoute | 1465 | `基础设置已完成。` | 尚可 | 不变 |
| SetupRoute | 1469 | `你现在可以进入管理页面，继续维护机器人、系统集成和存储清理。` | 过于罗嗦 | `你可以在管理页面继续调整设置、查看存储状态。` |
| ClaudeProfileSection | 230 | `管理不同的 Claude 连接配置。` | GPT 分类描述 | `Claude 连接配置` |
| ClaudeProfileSection | 297 | `创建一套新的 Claude 连接配置` | 不自然 | `新建配置` |
| ClaudeProfileSection | 391 | `使用这台机器当前可用的 Claude 设置。` | 自言自语 | `系统默认的 Claude 连接` |
| ClaudeProfileSection | 423 | `填写新的 Claude 连接配置。` | GPT 味道 | `填写连接信息` |
| ClaudeProfileSection | 424 | `修改后保存会更新当前配置。` | 设计意图 | 删除 |
| CodexProviderSection | 233 | `管理不同的 Codex 连接配置。` | GPT 分类描述 | `Codex 连接配置` |
| CodexProviderSection | 300 | `创建一套新的 Codex Provider` | 不自然 | `新建配置` |
| CodexProviderSection | 394 | `使用这台机器当前可用的 Codex 设置。` | 自言自语 | `系统默认的 Codex 连接` |
| CodexProviderSection | 425 | `填写新的 Codex 连接配置。` | GPT 味道 | `填写连接信息` |
| CodexProviderSection | 426 | `修改后保存会更新当前配置。` | 设计意图 | 删除 |
| helpers.ts | 131 | `这一步只验证当前凭证可连接。` | 冗余说明 | 删除 |
| helpers.ts | 133 | `运行态仍在重连，实际使用请以连接状态恢复为准。` | 过于技术化 | `实际连接以运行状态为准` |
| helpers.ts | 134 | `如果刚切到另一个飞书 App，旧会话不会自动迁移，请到新机器人侧重新开始会话。` | 太长 | `切换到新应用后请在新机器人侧重新开始会话` |
| helpers.ts | 147 | `后续实际使用请以连接状态恢复为准。` | 同上 | 同上 |
| ui.tsx | 45 | `railOpen ? "收起分区导航" : "打开分区导航"` | 尚可 | 改为 `收起菜单` / `打开菜单`（Settings）或保留（Admin） |

### 1.2 视觉/设计问题

#### 主题与氛围
1. **渐变背景过于花哨** — `radial-gradient` + `linear-gradient` 叠加，移动端渲染差，且在暗色模式下无意义
2. **玻璃拟态过度** — `backdrop-filter: blur(16px)` 几乎在每个面板上，遇到复杂背景产生不可控的叠加效果
3. **深色侧边栏 + 浅色主区割裂** — `.side-rail` 用 `rgba(13,29,41,0.92)` 几乎全黑，主内容区是白色系，视觉上完全是两个产品
4. **teal (`#0f766e`) 主色偏小众** — 配合深色侧边栏显得不够通用

#### 形与空间
5. **圆角严重不统一** — `1.75rem / 1.5rem / 1.35rem / 1.25rem / 1.2rem / 1.1rem / 1rem / 0.95rem` 到处混用，无规律
6. **按钮全 Pill Shape (`999px`)** — 在大屏尚可，小屏极占纵向空间，通用感差
7. **阴影过于单一沉重** — `0 24px 80px` 作为唯一的主要阴影
8. **卡片间距不统一** — 有的用 `gap: 1rem`，有的用 `1.2rem`，有的 `0.85rem`

#### 字体与层级
9. **字号跳跃严重** — h1 `clamp(1.75rem, 4vw, 2.4rem)`、h2 `clamp(1.8rem, 4vw, 2.7rem)`、h3 `1.25rem`、h4 `1rem`，h2 实际上比 h1 还大（Admin 场景）
10. **品牌 kicker 太小** — `font-size: 0.72rem` 实际上不可读

#### 组件
11. **StatCard/StatGrid 只在少数场景用** — 设计了一套统计卡片组件但大部分页面不用，浪费代码
12. **setup step rail 视觉单调** — 就是一排 button 列表，没有进度指示、没有分段提示
13. **loading/error 状态设计粗糙** — 居中一个脉冲点 + 文字，没有品牌感
14. **Modal 半透明背景 `rgba(9,18,27,0.58)` 在移动端看起来很脏**

### 1.3 移动端适配问题

#### 断点
15. **只有两个响应式断点** — `1080px` 和 `720px`，缺少中间态和更小的断点
16. **720px 断点把所有布局粗暴变成单列全宽** — 丢失信息层次，按钮变成全宽后占满整屏

#### 布局
17. **Side rail 的 toggle 机制体验差** — 在 1080px 下变成可折叠菜单，但 toggle 按钮只是一个文字按钮，视觉反馈弱
18. **Setup step rail 在移动端直接堆在顶部** — 占据 1/3 的屏幕高度才能看到当前步骤内容
19. **二维码扫描区在小屏不缩放** — QR 图保持固定宽度，可能超屏

#### 交互
20. **触摸热区偏小** — 按钮只有 `0.82rem` padding，无针对移动端的扩大
21. **表格在移动端直接横向滚动** — 没有转换成卡片视图

---

## 二、设计目标

1. **专业** — 界面看起来像一个成熟的运维/管理后台，而非原型/MVP
2. **干净** — 减少视觉噪音，统一间距和圆角，去掉花哨效果
3. **自适应** — 在 320px ~ 2560px 宽度范围内都能良好呈现
4. **简洁文案** — 每段文字要么指导操作，要么描述状态，不说废话

---

## 三、设计方案

### 3.1 色彩系统

```
品牌色（蓝）：
  --accent:           #2563eb   (主色)
  --accent-hover:     #1d4ed8   (悬停)
  --accent-subtle:    #eff4ff   (浅底色)

中性色：
  --surface:          #ffffff   (卡片背景)
  --surface-subtle:   #f8f9fb   (页面背景 / 次级区域)
  --border:           #e2e5ea   (边框)
  --border-light:     #eef0f3   (轻边框)

文字：
  --text-primary:     #111827   (主文字)
  --text-secondary:   #5f6b7a   (辅助文字)
  --text-muted:       #9ca3af   (禁用/占位)

语义色：
  --success:          #059669   (成功)
  --success-bg:       #ecfdf5   (成功背景)
  --warning:          #d97706   (警告)
  --warning-bg:       #fffbeb   (警告背景)
  --danger:           #dc2626   (危险)
  --danger-bg:        #fef2f2   (危险背景)

圆角：
  --radius-sm:        6px       (输入框、标签、按钮)
  --radius-md:        10px      (卡片、面板)
  --radius-lg:        14px      (大面板、模态框)

阴影：
  --shadow-sm:        0 1px 2px rgba(0,0,0,0.04)
  --shadow-md:        0 4px 12px rgba(0,0,0,0.06)
  --shadow-lg:        0 8px 32px rgba(0,0,0,0.08)
```

原则：放弃 teal 和玻璃拟态，改用蓝 + clean card + 分层阴影。背景改为纯色 `#f8f9fb`。

### 3.2 布局系统

#### Admin 页面布局

```
┌──────────────────────────────────────────────┐
│  Header: Logo + 产品名 + 版本号              │
├──────────────────────────────────────────────┤
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │ 机器人管  │  │ 系统集成  │  │ 存储管理  │  │
│  │ 理        │  │          │  │          │  │
│  │  [列表]   │  │ [自动运行]│  │ [预览]   │  │
│  │  [详情]   │  │ [VS Code]│  │ [图片]   │  │
│  │           │  │          │  │ [日志]   │  │
│  └──────────┘  └──────────┘  └──────────┘  │
│                                              │
│  ┌──────────────────────────────────────────┐│
│  │  Claude 配置                             ││
│  │  [列表] [详情/表单]                      ││
│  ├──────────────────────────────────────────┤│
│  │  Codex 配置                              ││
│  │  [列表] [详情/表单]                      ││
│  └──────────────────────────────────────────┘│
└──────────────────────────────────────────────┘
```

- 不再用 side-rail（Admin 场景不需要侧边导航，section 数量少）
- Header 固定顶部，main 区域可滚动
- 机器人管理区用 card 布局：左侧滚动卡片列表，右侧详情/表单
- 存储管理区三个 card 横排
- Claude/Codex 配置各一个 panel

**移动端 (<768px)：**
- Header 压缩，只显示 Logo + 标题
- 机器人列表变成横向滚动的 chip 列表
- 存储三个 card 进入可折叠 accordion
- 表单单列
- Claude/Codex 配置的列表/详情变成层叠式（选中后全屏进入表单）

#### Setup 页面布局

**桌面 (>1024px)：保持当前左右两栏**
```
┌──────────────────────────────────────────────┐
│  Header: 产品名 + 版本                       │
├──────────┬───────────────────────────────────┤
│  步骤 1  │  [当前步骤内容区]                 │
│  ──────  │                                   │
│  步骤 2  │  标题                             │
│  ──────  │  描述                             │
│  步骤 3  │  操作区                           │
│  ...     │                                   │
└──────────┴───────────────────────────────────┘
```
左侧 step list 改为带连接线的多层进度指示器。

**移动端 (<768px)：变为全屏步骤式**
```
┌───────────────────┐
│  ◀ 返回  步骤 3/9 │  ← 顶部导航条
├───────────────────┤
│  权限检查         │  ← 当前步骤标题
│  确认飞书应用权限  │
├───────────────────┤
│  [当前步骤内容]    │  ← 全屏内容
├───────────────────┤
│  [← 上一步][下一步 →] │  ← 底部导航
└───────────────────┘
```

移动端不再显示步骤列表，改为顶部步骤指示器（`步骤 3/9` + 返回/继续）和底部导航按钮。

**平板 (768px~1024px)：积累式布局**
- 步骤列表变为可折叠的 accordion
- 内容区占大部分宽度

### 3.3 组件重设计

#### 按钮

```
.primary-button {
  height: 42px;
  padding: 0 20px;
  border-radius: 6px;       /* 不再是 999px */
  background: var(--accent);
  color: white;
  font-weight: 500;
  border: none;
}

.ghost-button {
  height: 42px;              /* 统一高度 */
  padding: 0 16px;
  border-radius: 6px;
  background: transparent;
  border: 1px solid var(--border);
  color: var(--text-primary);
}

.danger-button {
  height: 42px;
  padding: 0 20px;
  border-radius: 6px;
  background: transparent;
  border: 1px solid var(--danger);
  color: var(--danger);
}
```

- Hover: 微调背景色，不再用 `translateY(-1px)`
- Disabled: `opacity: 0.4`
- 移动端最小触摸高度 `44px`

#### 卡片/Panel

```
.panel {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);    /* 统一 10px */
  box-shadow: var(--shadow-sm);
  padding: 24px;
}
```

- 去掉 backdrop-filter
- 统一圆角为 `var(--radius-md)`

#### 表单

```
.field input {
  height: 42px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: white;
}

.field input:focus {
  border-color: var(--accent);
  outline: none;
  box-shadow: 0 0 0 3px rgba(37, 99, 235, 0.1);
}
```

- 添加 focus ring
- 统一输入框高度

#### 步骤指示器（Setup 移动端）

```
[1] ─── [2] ─── [3] ─── [4] ─── ...
环境    飞书    权限    事件
```
- 水平排列，已完成的绿色，当前步骤蓝色高亮
- 超出宽度时水平滚动

#### 表格 → 卡片（移动端）

```
// 桌面
│ 事件名      │ 用途              │
│ im.message  │ 消息事件           │

// 移动端 (<640px)
┌─────────────────────────┐
│ 事件名                   │
│ im.message    [复制]    │
│ ─────────────────────── │
│ 用途                     │
│ 消息事件                 │
└─────────────────────────┘
```

#### 通知横幅

```
.notice {
  padding: 12px 16px;
  border-radius: 6px;
  border-left: 4px solid;
}

.notice.success {
  background: var(--success-bg);
  border-color: var(--success);
  color: var(--success);
}
```

改为左边框强调 + 轻背景，不再全色块。

### 3.4 响应式断点

```
> 1024px  : 桌面标准布局
768-1024px: 紧凑桌面 / 平板
480-768px : 大屏手机 / 小平板
< 480px  : 手机
```

**各组件在不同断点的行为：**

| 组件 | >1024 | 768-1024 | 480-768 | <480 |
|------|-------|----------|---------|------|
| Admin header | 标准 | 标准 | 压缩高度 | 压缩高度 |
| 机器人列表+详情 | 左右两栏 | 左右两栏 (ratio 1:1.5) | 列表竖排 + 详情叠下方 | 同左 |
| 系统集成 cards | 2列 | 2列 | 1列 | 1列 |
| 存储管理 cards | 3列 | 2列 | 1列 | 1列 (accordion) |
| Claude/Codex 配置 | 左右两栏 | 左右两栏 (ratio 1:1.5) | 列表竖排 + 表单叠下方 | 同左 |
| Setup 步骤 | 左侧固定 200px | 顶部 accordion | 顶部步骤条 | 顶部步骤条 |
| Setup 内容 | 右栏 | 全宽 | 全宽 | 全宽 |
| 表格 | 标准 | 标准 | 卡片化 | 卡片化 |
| 按钮 | inline | inline | 部分变为 block | block 全宽 |
| 表单 | 2列 | 2列 | 1列 | 1列 |
| 字体大小 | 默认 | -5% | -8% | -10% |

### 3.5 字体系统

```
--text-xs:    0.75rem   (12px)  辅助标签、badge
--text-sm:    0.875rem  (14px)  描述文字、placeholder
--text-base:  0.9375rem (15px)  正文
--text-lg:    1.0625rem (17px)  card 标题
--text-xl:    1.25rem   (20px)  panel 标题
--text-2xl:   1.5rem    (24px)  页面标题
--text-3xl:   1.75rem   (28px)  主标题
```

用 `clamp()` 在小屏自动缩小。不再用 `clamp(1.8rem, 4vw, 2.7rem)` 这种大范围的。

### 3.6 过渡与动画

去掉 `translateY(-1px)` hover 效果，改为更隐式的背景色过渡：

```css
transition: background-color 150ms ease, border-color 150ms ease;
```

---

## 四、实施计划

### 阶段一：样式系统重构（1 个文件）

**文件：`web/src/styles.css`**

1. 重写 `:root` 颜色变量、圆角、阴影
   - teal → blue-gray
   - 去掉 backdrop-filter 效果
   - 去掉花哨渐变背景
2. 重写按钮样式（pill → rect，统一高度，添加 focus ring）
3. 重写 Panel/Card 样式（统一圆角、去除 blur）
4. 重写表单/输入框样式（统一高度、添加 focus ring）
5. 重写通知横幅（左边框风格）
6. 重构响应式断点（1024/768/480）
7. 重写各组件在不同断点的布局规则
8. 添加 setup 移动端步骤条样式
9. 表格转卡片样式
10. 微调字体

### 阶段二：文案修改（6 个文件）

**文件：`web/src/routes/AdminRoute.tsx`**
- 修改所有 `<p>` 级别的 section description
- 修改 button label
- 修改字段 label

**文件：`web/src/routes/SetupRoute.tsx`**
- 修改 9 个 step 的 subtitle 和 notice 文案
- 修改操作提示
- 修改 step rail 引导文字

**文件：`web/src/routes/admin/ClaudeProfileSection.tsx`**
- 修改 panel description
- 修改 create/edit 引导文字

**文件：`web/src/routes/admin/CodexProviderSection.tsx`**
- 修改 panel description
- 修改 create/edit 引导文字

**文件：`web/src/routes/shared/helpers.ts`**
- 修改 `buildAdminFeishuVerifySuccessMessage` 和 `buildSetupFeishuVerifySuccessMessage` 返回的文案

**文件：无其他 .tsx 文件需要大改**

### 阶段三：组件结构微调

**文件：`web/src/routes/SetupRoute.tsx`**
- 移动端 step rail 改为横向步骤条（仅在 JSX 层面加条件渲染，不改变逻辑）
- 添加底部导航按钮（移动端）

**文件：`web/src/routes/AdminRoute.tsx`**
- 可能需要用 section 包裹某些区域来适配新的卡片布局

**文件：`web/src/components/ui.tsx`**
- 如需要新增组件（移动端步骤指示器等）

### 阶段四：测试与验证

1. 运行 `npm run test` 确保不破坏现有功能
2. 在不同宽度下视觉验证
3. 检查文案（逐句读一遍）

---

## 五、移动端防溢出要点（Mock 验证结论）

在 mock 制作过程中发现并修复了以下移动端排版问题，实现时必须遵守：

### 5.1 必要的基础规则

```css
body {
  overflow-x: hidden;
  word-break: break-word;
  overflow-wrap: break-word;
  min-width: 320px;
}
```

这三条是防止任何内容横向出框的底线。

### 5.2 Grid/Flex 内容截断

所有 grid 列模板必须使用 `minmax(0, 1fr)` 而非直接 `1fr`：

```css
/* 正确 */
grid-template-columns: 240px minmax(0, 1fr);

/* 错误 — 子内容可能撑破列宽 */
grid-template-columns: 240px 1fr;
```

所有 grid/flex 子元素需要 `min-width: 0` 才能触发内容截断（而非溢出）：

```css
.robot-layout > *,
.config-layout > *,
.step-stage,
.qr-layout > * {
  min-width: 0;
}
```

### 5.3 按钮移动端规则

桌面端按钮 `white-space: nowrap` 可以保留，但在 `<480px` 断点下：

- 按钮取消 `white-space: nowrap; flex-shrink: 0`
- `btn-row` 改为 `flex-direction: column`，按钮 `width: 100%`
- 最小触摸高度 `42px`（内联小按钮如表格中"复制"按钮除外）

### 5.4 表格移动端规则

表格在窄屏不强制转卡片，改为 `overflow-x: auto` + `min-width` 底线：

```css
.table-wrap {
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}
.table-wrap table {
  min-width: 360px; /* 再窄就横向滚动而非挤碎 */
}
```

表格内的 `code` 或长字符串需要 `word-break: break-all`。

### 5.5 长 Monospace 文本

scope pill、App ID、错误码等使用 `font-family: monospace` 的元素，必须加：

```css
word-break: break-all;
```

因为 monospace 字体在中英文混排时不会在 CJK 字符处自然断行，不处理会导致竖排每行一字。

### 5.6 固定宽度元素

- 避免在 `<768px` 下使用 `grid-template-columns: 1fr 240px`（QR 布局的右侧栏），改为单列 `1fr`
- 所有带固定 `px` 宽度的子列在对应断点下必须 collapse

### 5.7 无关联的空列 / 隐藏元素

`visibility: hidden` 的元素仍然占据布局空间。在响应式布局中需确保子元素 `display: none` 或用 `grid-template-columns: 1fr` 消除空列占位。

---

## 六、相对路径约束（反代环境兼容）

### 6.1 背景

Codex Remote 在生产环境中运行在反向代理后面，URL 前缀不可预期（如 `/g/app-id/`）。所有前端资源访问和 API 请求必须使用相对路径。

### 6.2 现有机制（必须保持）

项目已有完善的相对路径机制，位于 `web/src/lib/paths.ts`：

- **`relativeLocalPath(value)`** — 将任何路径转换为当前 mount 下的相对路径。例如 `/api/admin/apps` → `./api/admin/apps`。同时处理 mount 前缀场景（`/g/xxx/api/admin/apps` → `./api/admin/apps`）
- **`currentMountPrefix()`** — 检测当前 URL 的 mount 前缀（如 `/g/xxx/`），无前缀时返回 `/`
- **`relativePathWithinCurrentMount()`** — 判断给定路径是否在当前 mount 内并转为相对路径

API 层 (`web/src/lib/api.ts`) 已通过 `resolveRequestPath()` → `relativeLocalPath()` 自动转换所有请求路径：

```
所有 fetch() 调用 → resolveRequestPath(path) → relativeLocalPath(path) → 相对路径
```

Vite 构建配置 (`web/vite.config.ts`) 已使用 `base: "./"` 确保静态资源使用相对路径。

### 6.3 实现约束

实施重构时必须遵守：

1. **API 请求** — 所有 `fetch()` 调用必须经过 `resolveRequestPath()` 或 `relativeLocalPath()`，<mark>不能直接写死绝对路径 `/api/...`</mark>
2. **页面路由链接** — 不写死 `/admin` 或 `/setup`，使用相对路径 `./admin` 或通过 `relativeLocalPath()` 转换
3. **静态资源引用** — 不在 HTML/JSX 中使用绝对路径引用资源（`/assets/...`），保持相对路径 `./assets/...`
4. **新增 `fetch` 调用** — 如果重构过程中需要新增 API 调用，确保路径通过 `relativeLocalPath()` 处理
5. **不引入任何以 `/` 开头的硬编码 URL** — 包括 `<a href="...">`、`<img src="...">`、CSS 中的 `url(...)`、`import()` 等

### 6.4 验证方法

构建后在以下场景测试：

```
直接访问:           http://host:port/admin/
反向代理根挂载:      https://proxy.example.com/g/app-id/admin/
反向代理子路径挂载:   https://proxy.example.com/prefix/admin/
```

---

## 七、不在范围内的改动

以下保持不动：

- **所有 API 调用逻辑、状态管理、事件处理**
- **所有 TypeScript 类型定义**
- **路由逻辑、认证逻辑**
- 测试文件（除非文案断言需要更新）
- 构建配置
- Go 后端代码
- Logo / 品牌资源

---

## 八、风险与注意点

1. **测试文件可能有文案断言** — 修改文案后需要同步更新测试中的文本匹配
2. **CSS 改动范围大** — 需要逐屏验证所有页面/状态，确保没有遗漏的样式
3. **移动端步骤条是新增 UI 模式** — 需要确保不影响现有的 step 导航逻辑
4. **Claude/Codex 配置项的"声明式"文案** — 这些文案同时出现在按钮标签、描述、placeholder 等位置，需要逐处替换保持一致性
5. **相对路径不得破坏** — 所有 `fetch`、`<a>`、`<img>` 的 URL 保持相对路径，重构后必须在反代环境下验证
6. **CSS `min-width: 0` 和 `minmax(0, 1fr)` 是防溢出的核心机制** — 不可为了简洁而回退到普通 `1fr`
