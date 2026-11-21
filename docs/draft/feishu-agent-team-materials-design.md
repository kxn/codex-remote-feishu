# 团队与项目共享资料：API 与资料合同附录

> Type: `draft`
> Updated: `2026-11-21`
> Summary: 作为资料专项附录保留飞书原生入口、版本和并发接口证据；产品流程及总体技术合同统一指向两份主文档。

文档定位：本文件是专项依据／附录，不是另一份实施入口。用户行为以[产品主文档](./feishu-agent-team-product-design.md)为准，领域所有权、状态与迁移以[技术主文档](./feishu-agent-team-technical-design.md)为准，后端具体时序见[原生运行合同](./feishu-agent-team-runtime-contracts.md)。本文件保留原调查证据和解释；实现拆分、异常处理及验收统一按主文档的 US／T／UAT 编号追踪。

## 1. 结论与核查范围

可以用现有 Claude Code／Codex shell、多身份 CLI、飞书应用凭据及 API 实现持续维护的团队／项目资料，不需要强制绑定用户 OAuth，也不需要重造在线文档编辑器。主要缺口是资料身份、版本与同步领域，而非模型写文档的能力。

本轮核查了仓库生产代码、当前 SDK 以及飞书官方接口正文，未在真实租户创建或修改资料。这里的“可接线”指接口、身份和替代分支明确，不代表端到端功能已完成。实现必须完成第 10 节验收；不以未开启权限为理由删减产品。

约定：**同一资料持续更新同一个入口；每项事实只有一个权威来源；声明固定版本的交付须有固定依据，当前页面／观察数据集如实标明；团队共享不等于复制到所有项目；资料不是记忆，也不是执行队列。** 本文定义的是用户开发项目时的资料管理，不是 fsChannel 自身 docs 目录规范。

## 2. 已核实的 API 接线

| 能力 | 接口与身份事实 | 接入方式与边界 |
| --- | --- | --- |
| Docx 建立与维护 | [创建文档](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/create)、[批量更新块](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document-block/batch_update) 支持应用身份 | 保存 document ID，之后按 block 更新同一文档；创建与更新分开，不能每轮重新导入 Markdown |
| Docx 读取与历史 | [基本信息](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/get) 返回 revision；[所有块](https://open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/list) 可指定历史 revision，读历史需编辑权限 | 分页读取完整 blocks，纯文本仅供快速摘要；保存源版本及自有验收快照 |
| Wiki 组织 | [创建知识空间](https://open.feishu.cn/document/server-docs/docs/wiki-v2/space/create) 不支持应用身份；[创建节点](https://open.feishu.cn/document/server-docs/docs/wiki-v2/space-node/create) 可在有容器编辑权的既有空间／父节点下进行 | 人选择已有空间或父节点，Agent 建项目子树；纯 Bot 不承诺创建空间 |
| Wiki 应用访问 | [Wiki FAQ](https://open.feishu.cn/document/server-docs/docs/wiki-v2/wiki-qa) 说明含 Bot 的群可获空间成员／管理员身份，节点也可添加文档应用；搜索 Wiki 有应用身份限制 | token／节点树解析加本地资料索引，不依赖全租户 Wiki 搜索；Wiki 节点与实际 obj_token 分别记录 |
| Sheets | [范围写入](https://open.feishu.cn/document/server-docs/docs/sheets-v3/data-operation/write-data-to-a-single-range) 支持应用身份、指定 sheet/range；已有内容会被覆盖 | 用于计算、分析和矩阵；读改目标范围，保护公式和人工列；响应 revision 不等于条件写入锁 |
| Bitable | [更新记录](https://open.feishu.cn/document/server-docs/docs/bitable-v1/app-table-record/update) 按 app/table/record ID 更新 fields | 用于业务台账、测试用例、资料目录；尊重已有 schema，不把所有字段变成系统所有 |
| 命名版本 | Drive [创建版本](https://open.feishu.cn/document/server-docs/docs/drive-v1/file-version/create)／[版本列表](https://open.feishu.cn/document/server-docs/docs/drive-v1/file-version/list) 支持应用身份及 Docx／Sheet，创建为异步 | 可为里程碑附可读名称；不能用“请求已受理”当版本已生成，不能与 Docx 编辑 revision 混号 |
| 变更通知 | [订阅云文档事件](https://open.feishu.cn/document/server-docs/docs/drive-v1/event/subscribe) 支持应用身份，需文档 owner/manager；[编辑事件](https://open.feishu.cn/document/ukTMukTMukTM/uUDN04SN0QjL1QDN/event/file-edited) 提供对象／操作者等信息 | 事件只通知可能变化，再补读；补 Webhook 入口或使用资源对账，不能假设当前长连接直接覆盖全部事件 |

GitHub 侧可通过已配置 GitHub App／仓库凭据接 [issue 更新](https://docs.github.com/en/rest/issues/issues#update-an-issue)、[PR 查询](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request)、[按 ref 读仓库内容](https://docs.github.com/en/rest/repos/contents#get-repository-content)。Agent 沿用 Git／gh 的仓库工作流，connector 保存 issue／PR ID、正文采纳版本、HEAD 和合并回执；本地无远端仓库可直接按 commit 读文件。GitHub 凭据独立于飞书 Bot，不因接入飞书就假设仓库可访问。issue 正文读改写也不视为强 CAS：协调系统写入、回读人工变更，冲突时提交建议而非全篇覆盖。

应用身份访问资源需要 API scope 和该资源上的访问权；Wiki 单页可编辑不等于父节点可创建子页。这是数据接入合同，不是要求本轮先完成权限调试。资料维护使用绑定的应用凭据，不让模型自己挑一个权限更高的 Bot 重试。

无现成 Wiki 时，默认在应用管理的云空间资料根建立固定 Docx 首页和资料集合，群内链接这个首页；人以后选择 Wiki 后关联或迁移导航，保留 ResourceID。目录容器来自已有授权文件夹或由应用创建并共享的文件夹，不要求用户逐个建项目文档。Wiki 不可用不阻塞 Docx／Sheets／Bitable 工作。

### 2.1 群原生固定入口

本轮再次读取官方 API 正文与帮助中心，确认可用应用身份实现原生入口，不以 User OAuth 为前提。

| 原生能力 | 已核实接口／行为 | 采用方式 |
| --- | --- | --- |
| 群顶部置顶 | [更新群置顶](https://open.feishu.cn/document/server-docs/group/chat/put_top_notice) 可置顶群内消息或群公告，支持 tenant token；操作者须符合群设置 | 发布短导航消息并置顶，让人直达项目首页；不是置顶用户个人会话列表 |
| Pin 消息 | [Pin](https://open.feishu.cn/document/server-docs/im-v1/pin/create) 是独立的消息收藏能力；已 Pin 重复调用返回原 Pin 信息 | 保存导航或重要讨论入口，不能把 Pin 说成顶部置顶 |
| 自定义 Tab | [创建](https://open.feishu.cn/document/server-docs/group/chat-tab/create)、[查询](https://open.feishu.cn/document/server-docs/group/chat-tab/list_tabs)、[更新](https://open.feishu.cn/document/server-docs/group/chat-tab/update_tabs)、[删除](https://open.feishu.cn/document/server-docs/group/chat-tab/delete_tabs)、[排序](https://open.feishu.cn/document/server-docs/group/chat-tab/sort_tabs) 均支持应用身份；创建／更新／删除限 doc、url 类型 | 默认一个“项目”文档 Tab，指向同一首页；需要时再关联资料或成果入口 |
| 系统群文件／云文档 Tab | [官方客户端说明](https://www.feishu.cn/hc/zh-CN/articles/536850681075) 说明按会话内容生成并分类查看消息；返回的 file/doc_list 等枚举不是创建接口支持范围 | 用于自然浏览聊天资料，不伪造专属群文件目录 API，也不承担资料版本／永久保存 |

添加 doc Tab 使用现有云文档 URL，官方示例包括 Wiki 链接；Bot 需要文档协作者权限。标签加入不等于全群自动获得正文访问权。url Tab 可配置内嵌打开，但不提供托管、鉴权或数据隔离；不因此要求新建独立 Web 门户。

API 指明 Tab 支持 group／p2p 模式，官方帮助指出话题群不支持会话标签；普通群中的话题回复与话题群不是同一概念。一个会话最多 20 个自定义 Tab，设计使用少量稳定入口，不每任务建 Tab。具体群类型／平台不支持某入口时保留普通导航消息及已有可用入口，不把缺少 Tab 当作项目无法工作。

IM 附件下载使用 message_id＋file_key，[Drive 文件夹列表](https://open.feishu.cn/document/server-docs/docs/drive-v1/file/list) 使用 folder_token；群文件浏览不是二者自动映射的永久目录。本轮未找到用于任意增删系统群文件页条目的专门 API，不能将 ListTabs 的结果当文件列表。

入口接线：项目首页建立后，由对应 Bot 创建导航消息／doc Tab，保存 ProjectID、chat、binding generation、入口用途、message_id／tab_id 与目标 ResourceID。后续更新已有对象；超时重试先按已存 ID、确切目标链接和 ListTabs 核对，不能凭同名标题认领其他入口。现有 SDK 已有 ChatTab 五种操作，仓库尚无生产调用，新增薄 adapter 经 FeishuCallBroker 接入即可。

只维护本系统登记的入口，不重排或删除人的其他 Tab。既有顶部置顶可能属于项目之外的沟通；没有可靠当前占用证据或明确接管意图时，使用文档 Tab／导航消息，不盲目替换。群信息 Get 在本轮核查中未返回当前 top_notice，不能用本地旧记录声称已经核实当前顶部内容。入口被人修改／撤下时保留事实，不后台反复抢回；需要恢复由明确指令或既有管理约定触发。

置顶消息保持简短导航，状态内容主要在固定文档中更新，不把历史卡当永久状态数据库。首页共享内容按项目范围展示；个人私聊待办不写入群共享文档。入口、资源访问和通知送达分别记录，部分成功如实显示，不把创建 Tab 当作所有成员已可读。

## 3. 现有基础与新增职责

已有 [Drive adapter](../../internal/adapter/feishu/markdown_preview_lark_api.go)、[Bitable API](../../internal/adapter/feishu/bitable_api.go)、[附件下载落地](../../internal/app/daemon/app_inbound_file.go) 和 [文档评论读取](../../internal/app/daemon/tool_service_drive_comments.go) 可按职责复用。Docx 正文／blocks、Wiki 解析与节点、Sheets 单元格、事件入口、GitHub 资源绑定需要新增应用用例与 adapter。

旧预览服务于人逐次监督 Agent 的场景，可保留为辅助，不因目标改变就把它视为错误实现。当前预览缓存由 scope＋路径＋内容 hash 定位，内容变化会产生新上传；[预览清理](../../internal/adapter/feishu/preview/markdown_preview_managed.go) 会删除远端文件，默认后台清理年龄为 24 小时。长期资料必须使用不同远端目录、注册记录和清理范围；不能只给旧预览加一个永久标记却仍被旧目录扫描器删除。IM file_key 也不能当云文档的永久 ID。

只保留 Bitable 通用 API，不复用旧 Cron 的 schema、配置页、scheduler、运行记录和 cronrepo。项目已有业务表不能被 Cron schema 校正器改写。本仓库 issueworkflow 是开发本仓的工具，不自动等于产品的 GitHub connector。

建议新增 ResourceService：UI 与 CLI 共用登记、关联、读取、查询、更新、版本冻结、归档和取消关联用例。传输层沿用 broker／重试基础，外部副作用进入持久 operation；它不替 Agent 编写业务内容，也不代管 Git 分支。

## 4. 信息放在哪里

下表是可采用的组织建议。具体项目可以调整资料载体与流程，并随阶段变化；产品硬约束是已选来源、访问范围、保存结果和版本含义真实，不是强制所有产品说明进 Docx、所有工程事项进 issue。

| 内容／事实 | 可采用的来源 | 选用后其他位置怎样参与 |
| --- | --- | --- |
| 团队通用方法、术语、跨项目标准 | 团队飞书文档／知识库 | 项目引用，角色记忆保存索引；有意偏离时形成项目例外 |
| 产品愿景、业务需求背景、用户流程、会议决定、长期产品说明 | 项目飞书 Docx | issue 引用相关段落与采纳版本；群讨论留下决定入口 |
| 工程需求、缺陷、执行范围、验收条件及业务处理状态 | 明确选择以 issue 管理的事项才以 issue 为权威；其余按项目选择内部 Task／已接入业务记录 | 一个工作项只选一个业务状态写源；内部 Task 仍保存 Agent 分派、attempt、阻塞和依赖 |
| 代码差异、审查、CI、是否合并 | PR／Git | issue 关联实现；飞书展示链接、结果与版本，不另记一个可独立改写的“已合并” |
| 与代码版本共同演进的架构、接口契约、部署运维、配置与贡献规范 | repo docs、README、AGENTS.md 等 | 按仓库工作约定更新；需要固定版本时标 commit，当前 ref 则明确为动态来源 |
| 业务记录、测试用例库、测试结果索引、资料登记、资源台账 | Bitable | 每表定义字段 owner，必要时链接 issue／PR／资料；不充当 runtime 数据库 |
| 需要公式、预算计算、分析模型、比较矩阵 | Sheets | 保留 sheet/range 和计算依据；需要复现计算时保存必要公式、输入和结果依据 |
| 原图、附件、导出报告、构建产物 | 持久文件存储或相应制品系统 | 资料目录登记 ResourceID；群内附件是分发副本，不是唯一原件 |
| 角色经验、摘要、恢复线索 | 作用域明确的 MemoryEntry／检查点 | 引用权威资料与版本；不能成为另一份隐藏需求或规范 |
| 原生执行、inbox、outbox、租约、重试 | 本地事务工作库 | 飞书表格只展示业务进度，不参与任务领取事务 |

同一项事实不能同时由 issue 正文、飞书表格和内部 Task 任意独立改写。对 issue-backed Task：外部 issue 的范围／验收／业务状态是权威，内部执行状态是另一类事实；Agent 从内部提交业务变更时先记操作意图，写入 issue 并回读后确认。失败显示待同步，不假装已发布。外部人工关闭、重开或修改 issue 会转成工作输入，而非偷偷用旧内部字段覆盖。关闭记录“外部已关闭”，按照项目采用的状态语义处理在途工作；不能把关闭事实自动解释成用户验收。重开通知 owner 依据最新范围与已有成果重新安排，不盲目重放副作用。

PR 合并不必然等于项目验收；关闭 issue 也不必然证明已有 PR 合并。Bitable 中显示的 issue／PR 状态为投影，用户的“暂停这项工作”等操作通过命令／请求字段进入领域用例，不能改展示字段就绕过校验。

不把整份 PRD 复制进 issue。PRD 负责背景和整体行为，issue 保存本项明确采纳的范围／验收及来源版本。二者冲突时标记需修订；当前已批准工作不因在线 PRD 有人随手改了草稿便自动改目标。负责人处理范围变更并通知角色。

不把所有技术讨论强塞进 repo：尚在探索的取舍可在飞书讨论；项目要求随代码版本成立的接口、架构与操作说明，可由 repo 文档承担并按仓库流程更新。项目选择飞书维护的说明则以该处为原件；引用和只读预览不能成为悄悄覆盖原件的第二写源。

## 5. 资料身份、范围与版本

### 5.1 团队与项目

`Team` 是用户选择复用的一组同事及共享资料集合，独立于某个飞书群和 Bot；不是推断出的全租户组织，也不是新的模型配置层。项目明确选择所属团队，引用其通用资料；资料访问仍须有项目关联与底层访问权。跨团队共享要显式建立关联，不能因复用了同一个 Bot 自动开放全部资料。

共享范围至少分为 team、project、personal。每项资源有一个内容 owner，可被多个已授权集合引用。项目默认能使用已关联团队资料，不因此获得修改通用规范的权力；变更团队规范按团队既有授权由负责人处理。项目覆盖规则独立记录并链接来源，不复制整个团队知识库。

新项目引用活跃团队资料，记录当前采用版本；资料更新会提示受影响项目，任务仍固定自己的需求／验收基线。可重复实验、历史版本维护等场景允许项目显式锁定团队资料版本，界面显示有更新可采纳。

### 5.2 最小对象合同

| 对象 | 必需字段与职责 |
| --- | --- |
| ResourceCollection / Binding | team/project/personal、目录结构、资源引用、适用范围与绑定代次 |
| Resource | 稳定内部 ID、标题、类型、owner scope、canonical source、远端 locator、内容负责人、connector binding、当前已观测版本、可用性 |
| ResourceVersion | ResourceID、source revision／Git commit／内容 hash、抓取时间、来源锚点、持久快照／观测数据集、完整性与一致性等级 |
| ResourceOperation | 发起 actor/run、expected baseline、受影响块／字段、幂等键、外部回执、不确定状态与对账结果 |
| ResourceMaterialization | Agent 本地可读副本、对应版本和缓存状态；可回收，不是原件 |
| ArtifactVersion | 本次交付固定的一个或多个 ResourceVersion／commit 与清单，供评审和验收 |

资源标题、群名、本地路径和 share URL 均不能单独作为身份。同一远端对象被两个 Bot 登记时按租户＋原生对象类型／token 去重，绑定范围另存；一篇文档多个 Wiki 快捷入口可映射同一底层资源。不能仅因不同 AppID 就误建两个权威正文。

Git 资源保存 repository identity＋path；动态导航跟随明确 ref，需要固定成果版本时使用 commit，未提交演示则标明实际状态，不能冒充不可变版本；目录或文件移动记录映射和沿革。未知文件不是按同名自动合并，需检查来源／用户意图。

### 5.3 多 Bot 的访问身份

资料集合绑定稳定 connector identity，可使用负责人对应应用或专设资料应用；不要求每个角色重新创建自己的资料副本。角色调用先按 Team/Project/PersonalScope 检查访问，再由固定 connector 访问外部资源。应用是传输／持有者身份，真实编辑角色和发起人另外记入操作审计。

纯 Bot 接线不隐含人的 OAuth。若资源另有直接 Bot 编辑能力，也走相同写入协调和回执归并。凭据失效或更换 App 时重新核对资源访问；不能换一个应用 token 就声称资料所有权已转移。私聊个人资料不能复用项目公开文件夹。

## 6. 稳定更新与版本保留

### 6.1 用户可感知的合同

群里“更新需求说明”应更新原文档，并回复“已更新需求说明：补充导出权限，关联事项 X”，附同一个当前链接。重复执行不能产生“需求说明最终版2”一类副本。资料首页与目录行继续指向同一 ResourceID。

“当前资料”持续演进；需要固定版本的交付记录实际采用的资料与代码版本。是否为评审、验收或里程碑保存完整快照，由项目当前约定和具体需求决定；普通草稿讨论可以引用当前页面。历史入口必须如实区分固定版本与动态链接，不能伪称后者是当时内容。

### 6.2 发布与重试

1. 读取目标 Resource 与写入范围，核对 scope、源身份及基线。登记 operation，冻结真实发起者。
2. 已有资源更新原 locator；新资料才创建。登记 ResourceID 和创建意图先于远端请求。
3. API 支持 client_token 时重试复用同一个值；文档写入按块分批，完成后回读核对。
4. 保存响应、已观测版本、内容 hash 与操作结论，再更新目录和通知。外部成功但索引未成功显示“资料已更新，索引待刷新”，不重复创建资料。
5. 超时且不知道创建是否成功时进入对账，在受管父目录和操作标记中定位；无法可靠识别时保留不确定状态，不仅按标题认领，也不再次盲建。

Docx 的 `document_revision_id` 不是 expected-current 强 CAS；`client_token` 解决请求幂等而非人工并发。Bitable 无基于调用者版本的条件写入，且同一 table 的写操作需要串行。内部按外部文档 ID 或 app_token＋table_id 协调全部 Bot 写请求，Sheets 按 spreadsheet 协调；lease 过期先查旧请求是否已完成再重试。

内部排队无法锁住人在飞书的编辑。受管段落、系统字段和人维护区域明确分工；写前检查基线、精确写目标块／字段、写后回读。共同编辑区域变化时先生成变更建议，由负责角色基于最新内容整合。要求绝不覆盖人工并发编辑的内容使用追加建议／独立草案后人工采纳，不承诺自动 read-check-write 等同原子 CAS。这个分支是 API 边界的明确处理，并非让所有日常资料更新都等待批准。

不支持精确保留结构的块、图形或公式不能通过全篇纯文本替换绕过。保留未知结构并报告待处理部分；结果只有核实完整才标记成功。多块操作不宣称全篇原子更新，部分成功通过 operation 保存进度与补偿建议。

### 6.3 三种版本不能混淆

- Resource 本地版本：我们观测或发布的内容版本；跨载体可统一引用，不冒充远端事务号。
- 原生编辑版本：如 Docx revision、Sheets 响应 revision、Git commit、Bitable 读取结果 hash；不同载体保证不同。
- Drive 命名版本：异步建立的里程碑版本，有独立 ID；创建结果与列表核实后才关联，不能靠请求时间推定内容一致。

在项目要求完整可复核材料时，持久保存实际读到的结构化 blocks／目标数据／文件及 hash，与来源版本绑定；必要的图片／附件也纳入清单。ResourceVersion 记录完整性和一致性等级：固定 Docx revision 等有原生同版证据的结果可标为版本快照；没有一致快照保证的 Sheets／Bitable 分页读取标为“观测数据集”，记录抓取区间、范围与各项来源，不冒充某一时刻的原子快照。前后多次读到相同内容仍不能证明原子一致。

一般分析可明确交付并验收已固定的观测数据集。要求严格同一源版本的事项，采用已验证的版本导出机制，或经源负责人确认停止编辑及计算变化后形成可核对基线；不假设平台可自动锁住所有人工编辑。条件未满足显示“快照未就绪”。公式表保留公式、输入与计算结果及时间，不能只存截图或暗示不同时间读到的值必然对应。

不承诺飞书无限历史保留或跨表原子快照。只有约定完整快照的事项才受快照完整性要求约束；摘要可以是合法交付，但不能被称为完整原文快照。个人／项目资料及快照遵守同一共享范围；本地持久快照需备份，不放在可被 worktree 清理的目录。

### 6.4 面向人的发布与保存责任

[交付工作约定](./feishu-agent-team-human-delivery-design.md) 区分过程材料、待评审材料、正式成果及长期知识，受众另作一条轴。临时决策资料也可能需要稳定链接和版本，不能按“不是长期文档”就放回会被清理的旧预览区。

Resource／ArtifactVersion 负责内容身份与固定版本，Delivery 负责向谁、为何、需要什么动作以及通知是否送达。交付摘要不是第二份权威正文，待我处理视图不是另一个资料仓库。未处理的评审／决定和仍被引用的版本保持保留；关闭后按项目既定保留策略归档／清理，未设置策略时不套用预览 TTL。

正式交付先持久保存并登记，再通知；旧自动预览结果不能自动升级为正式 Resource 或完成证据。旧能力可保留为按需查看，具体存放、分类、完成检查和提示词职责见交付约定，不重新让模型决定每份已交付成果是否需要持久保存。

## 7. 变更发现、搜索与 Agent 上下文

事件驱动只作为刷新信号。[Bitable 记录事件](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/reference/drive-v1/file/events/bitable_record_changed) 不涵盖公式值变化；编辑事件也不是完整 diff。接收后去重、合并、持久登记，再读取资源；断连、重启、周期性资源对账和读前刷新覆盖遗漏。这是 connector 的一致性维护，不复用旧 Cron 或向用户暴露定时任务。

不能订阅的资源按需补读并显示最后核查时间；不可访问时保留原引用与带时间的缓存，不偷偷把缓存当最新。外部删除标记 unavailable/tombstone，不因为索引还有一行就声称原文存在。

索引先支持标题、类型、scope、标签和正文检索，结果携带出处、版本与更新时间；不得跨私聊／项目泄露命中摘要。向量检索可选，不是可靠资料管理的先决条件。Bot 无全局 Wiki 搜索能力时从已登记资源和节点树构建索引。

每次工作只注入资料目录与相关摘要，详细内容按需通过 CLI 读取。每份摘要附源版本；资源变更使旧摘要过期，并通知正在依赖它的工作流。新资料内容作为带来源的数据，不能自动升格为系统指令或覆盖已批准的任务目标。

对外部 issue 修改、PR 合并和 repo 更新同样读取当前事实；事件只唤醒刷新，不能仅凭一条 webhook 直接宣布工作完成。自发写入回流按 operation／源版本识别，不能再次触发同一更新循环。

## 8. 迁移、归档与删除

既有临时上传链接不自动都转成永久资料。Agent 依据仍在使用的项目资料和用户明确选择建立清单；能确认来源的文件迁入永久区并登记沿革，旧预览指向新资料入口。原 URL 不能保留时明确标注新链接，不承诺平台不存在的 URL 重定向。

迁移先从旧清理范围移走或复制到安全容器并核实完整，再宣布永久保存；不能等待下一轮清理后才补登记。代码仓库已有 docs 保持 Git 权威，不全量上传成可编辑 Docx。

项目归档保留资料、版本和引用历史；取消项目对团队资料的关联不删除原件。删除资源与取消关联是不同操作，有跨项目引用时说明影响。永久删除按明确用户意图处理，含快照、索引与派生记忆，不用“重置会话”代替资料删除。

Team／项目资料的寿命独立于原生会话、Bot 进程、群消息和 worktree；退出群、换模型、删临时目录都不能销毁资料。更换资料 connector 或主群保留资源身份，更新访问和绑定代次。

## 9. 实施接线顺序

1. 明确 Team/Scope、Resource 与权威源合同，建事务注册表、持久快照及独立永久目录，断开旧预览清理。
2. 增加 Docx／Wiki／Sheets connector，抽离通用 Bitable adapter，提供稳定登记、读取与更新；多身份 CLI 与 UI 调相同用例。
3. 接上版本固定、操作回执、创建对账、并发冲突和本地 materialization；Agent 通过资料 ID 工作。
4. 接上 issue/PR/repo 来源绑定、事件与补读、索引、记忆失效和受影响任务通知。
5. 实现完整产品中的项目建立、资料维护、交付和归档体验，迁移仍有价值的旧资料。

这些是依赖，不是缩成临时上传的分期产品。设计不强制每个项目都使用所有载体，但所声明支持的载体必须满足自己的完整合同。

## 10. 交付验收

以下验证产品能力，不规定用户所有项目必须采用同一流程。

- Bot 创建文档 Tab、导航消息及适用置顶／Pin；原入口更新、超时后重试和重启不制造重复 Tab，不覆盖人工入口。
- 不支持 Tab 的群保留真实可用入口；添加文档入口不伪造访问授权；群文件浏览不被当作持久保存回执。
- 同一项目从探索性讨论转为需要固定证据的评审，分别采用其当时约定；接入 GitHub 不强制所有事项创建 issue。

- 同一 Docx 连续修改三次，群和目录的当前链接不变；可查看某次验收的固定内容与出处。
- 一个团队规范被两个项目引用，更新能通知两边，但不自动改已冻结任务基线；项目例外不污染团队原件。
- 两个 Bot 同时写同一文档／表时按外部资源协调；人改受管内容时保留其修改或进入明确冲突，不以原位更新掩盖覆盖。
- Docx 图片／表格、Sheets 公式、Bitable 人工字段在局部更新后保持；不支持部分有具体反馈。
- 创建成功后断网／重启不会盲建第二份；更新部分成功可继续核查；异步命名版本未完成不显示已生成。
- 关闭订阅、丢失事件、公式变化和缓存过期后，读前／后台对账可发现变化，过期状态对用户可见。
- 纯 Bot 新项目在无 Wiki 情况可建固定资料入口；选已有 Wiki 后能解析、读写节点；不会要求隐式用户 OAuth。
- 人改 issue 范围、关闭／重开或合并 PR，执行事实与业务事实正确归并；飞书表格不能自造合并结论。
- repo 文档随 commit 可追踪，飞书只读投影不会反向覆盖代码；文档移动保留沿革。
- 旧预览清理、worktree 删除、会话重建不删除永久资料；个人资料、缓存和搜索结果不跨范围泄露。
- 解除项目引用保留团队原件；归档可追溯；明确删除时同步处理快照和索引。

## 11. 关联设计

- [完整产品设计](./feishu-agent-team-product-design.md)
- [技术总设计](./feishu-agent-team-technical-design.md)
- [运行与 Agent 自主管理 Git](./feishu-agent-team-execution-design.md)
- [现有代码复用审计](./feishu-agent-codebase-reuse-assessment.md)
