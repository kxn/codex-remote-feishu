# Authenticated External Access Foundation Design

> Type: `implemented`
> Updated: `2026-04-16`
> Summary: external-access 基座已落地到当前代码，文档记录独立 listener、`trycloudflare` provider、短时授权 URL、idle auto-destroy，以及 provider reuse 的 health/target 约束。

## 1. 文档定位

这份文档只解决 `#83` 当前真正要落的“基座问题”：

- 不是把现有 admin/setup 页面直接公网化。
- 不是替换现有 `.md` 上传到飞书云空间的预览链路。
- 不是直接交付某个具体 preview / review 页面。

第一阶段只做一件事：

- 为指定 localhost HTTP 服务提供一个独立的、带认证的外部访问底座。

这个底座必须同时满足两类调用：

1. 用户点击一个短时有效的外部链接，可以从公网访问到指定的本地页面。
2. 程序内部可以通过统一接口，把某个 localhost 目标 URL 变成一个短时有效的外部授权 URL。

## 1.1 当前确认的第一个真实 consumer

截至 `2026-04-10`，这条基座已经有了第一个明确使用用例：`#112 /debug admin`。

它带来两条额外约束，需要提前沉淀到基座层，而不是等 consumer 落地时再临时加：

1. 第一个 consumer 不是泛化 preview/review 页面，而是**短时暴露本地 admin 页面**，并从飞书会话里直接返回可点击链接。
2. 飞书消息里的外链存在被预览/预取的现实风险。当前没有足够明确的官方承诺可以证明“飞书绝不会在用户点击前请求链接”，因此基座必须按**链接可能被预取**来设计 token 交换语义。

这意味着 phase 1/2 的基座设计里，不能把外部授权 URL 里的初始 `?t=` token 设计成“第一次匿名 GET 就一次性烧掉”的模型，否则第一个 consumer 一接进来就会出现可用性问题。

## 2. 收敛结论

当前产品方向按下面结论收敛：

- 走独立 listener，不与现有 admin/setup listener 混用。
- provider 第一阶段选择 `trycloudflare`。
- 运行时需要随仓库发行物一起 bundle 一个 `cloudflared`，实现上优先收敛为主二进制内嵌、首次使用时解到当前进程旁边。
- 对外能力的 source of truth 是 daemon 内部的授权与 allowlist 规则，不是 consumer 自己拼 token。
- 面向公网的第一阶段只提供“有认证的 HTTP/WS 反代能力”，不承诺通用任意端口穿透。
- 第一个真实 consumer 按 `#112 /debug admin` 收敛，但 consumer 需求不应反向破坏基座边界。
- 初始授权链接必须按“可能被飞书/浏览器/安全组件预取”设计，不能依赖 first-GET 单次消费语义。

`gradio` 仍然可以作为将来某些页面自身的实现技术，但不再作为这条“通用外部访问基座”的 provider。

## 3. 为什么第一阶段不用 Gradio

这次要解决的是“让任意指定 localhost Web 目标在短时间内可被认证外部访问”，不是“分享一个 Gradio app”。

按官方文档，Gradio 的 `share=True` 和自定义 FRP server 语义都绑定在 Gradio app 生命周期里；它适合把 Gradio 服务分享出去，不适合作为仓库级通用反代入口。相比之下，`cloudflared tunnel --url <localhost>` 直接面向任意本地 HTTP 服务，更贴合这张单的需求。

因此这里的结论不是“以后仓库里不能用 Gradio”，而是：

- 基座 provider 选 `trycloudflare`
- 具体 consumer 页面是否用 Gradio，是上层实现问题

## 4. `trycloudflare` 的适用边界

官方文档给出的几个关键事实会直接影响接口设计：

- Quick Tunnel 只用于 testing / development，不承诺生产 SLA。
- `trycloudflare.com` 域名是随机生成的临时子域名。
- 并发上限是 200 in-flight requests，超限返回 `429`。
- Quick Tunnel 不支持 SSE。
- 如果用户目录里已经有 `.cloudflared/config.yaml`，Quick Tunnel 当前不受支持。
- `cloudflared` 自带 metrics endpoint，并提供 `/ready` 健康检查。

这意味着第一阶段必须明确：

- provider 是“临时公网入口”，不是稳定生产入口。
- 外部授权 URL 不能假设公网域名长期稳定。
- consumer 第一阶段不能依赖 SSE。
- daemon 启动 `cloudflared` 时必须隔离配置目录，避免用户现有 `.cloudflared` 污染 quick tunnel 行为。
- 这条通道默认是**短时临时资源**：如果 external-access listener 连续 `30m` 没有收到任何入站或出站流量，应自动销毁 listener + provider，并回收这次临时公网入口。

当前实现还额外收敛了两条 runtime 约束：

- provider reuse 不能只看“进程还活着”，还必须在复用前重新确认 `cloudflared` 的 `/ready` 仍健康。
- provider reuse 不能跨 listener target 漂移；如果本地 listener 重建后目标 URL 发生变化，provider 必须丢弃旧 tunnel 并重启。

## 5. 总体架构

建议把这套能力拆成 4 个明确组件：

1. `external-access service`
   - daemon 内部总入口
   - 负责签发 URL、持有 grants、协调 provider 与 proxy listener
2. `proxy listener`
   - 独立监听 `127.0.0.1:<externalAccess.listenPort>`
   - 负责 token 交换、cookie 建立、请求转发、header/cookie/redirect 重写
3. `provider`
   - 第一阶段唯一实现为 `trycloudflare`
   - 负责把 proxy listener 暴露到公网，并给出 `publicBaseURL`
4. `consumer-facing URL builder`
   - 给程序内部调用
   - 输入 localhost 目标 URL，输出短时有效的外部授权 URL

运行结构如下：

```text
consumer / daemon code
  -> external-access service.IssueURL()
      -> ensure provider public base
      -> create in-memory grant
      -> return https://<random>.trycloudflare.com/g/<grant-id>/?t=<token>

external user browser
  -> trycloudflare public URL
      -> cloudflared
          -> local proxy listener
              -> token exchange / cookie session
              -> reverse proxy to localhost target
```

实现上，这条链路现在按“三段式”推进 lifecycle：

1. App/Service 在锁内只摘出当前 listener/provider/runtime 状态，并决定是否需要 shutdown 或 restart
2. `server.Close()` / `listener.Close()` / `provider.Close()` 等慢操作在锁外执行
3. 完成后再回到锁内清理可见状态与短时 grant

这样 `App.mu` / `Service.mu` 不再直接包裹 external-access 的慢关闭路径。

## 6. 配置与状态模型

### 6.1 新增配置段

建议在 `config.AppConfig` 中新增：

```go
type ExternalAccessSettings struct {
	ListenHost                string                         `json:"listenHost,omitempty"`
	ListenPort                int                            `json:"listenPort,omitempty"`
	DefaultLinkTTLSeconds     int                            `json:"defaultLinkTTLSeconds,omitempty"`
	DefaultSessionTTLSeconds  int                            `json:"defaultSessionTTLSeconds,omitempty"`
	Provider                  ExternalAccessProviderSettings `json:"provider,omitempty"`
}

type ExternalAccessProviderSettings struct {
	Kind          string                    `json:"kind,omitempty"` // disabled | trycloudflare
	LazyStart     *bool                     `json:"lazyStart,omitempty"`
	TryCloudflare TryCloudflareSettings     `json:"tryCloudflare,omitempty"`
}

type TryCloudflareSettings struct {
	BinaryPath           string `json:"binaryPath,omitempty"`
	LaunchTimeoutSeconds int    `json:"launchTimeoutSeconds,omitempty"`
	MetricsPort          int    `json:"metricsPort,omitempty"` // 0 = auto
	LogPath              string `json:"logPath,omitempty"`
}
```

建议默认值：

- `listenHost = "127.0.0.1"`
- `listenPort = 9512`
- `defaultLinkTTLSeconds = 600`
- `defaultSessionTTLSeconds = 1800`
- `provider.kind = "trycloudflare"`
- `provider.lazyStart = true`
- `tryCloudflare.launchTimeoutSeconds = 60`
- `tryCloudflare.metricsPort = 0`

### 6.2 环境变量 override

建议补齐：

- `EXTERNAL_ACCESS_HOST`
- `EXTERNAL_ACCESS_PORT`
- `CODEX_REMOTE_EXTERNAL_ACCESS_PROVIDER`
- `CODEX_REMOTE_TRYCLOUDFLARE_BINARY`
- `CODEX_REMOTE_TRYCLOUDFLARE_LAUNCH_TIMEOUT`

这批 env 只做 runtime override，不改变“当前真实配置以 `config.json` 为准”的方向。

### 6.3 不落盘的状态

以下内容不建议持久化：

- 当前随机 `trycloudflare.com` 域名
- grant 列表
- grant token
- grant session

原因：

- 这些值本身就是短时临时态
- daemon 重启后 tunnel URL 通常变化
- grants 与 tunnel 生命周期天然绑定

因此 phase 1 的 source of truth 应该是进程内内存状态，而不是 state file。

## 7. cloudflared bundle 设计

### 7.1 为什么要 bundle

用户已经明确提出第一阶段倾向“自带 `cloudflared`”。这也是正确方向，因为：

- `trycloudflare` 不是仓库用户默认会手工安装的工具
- 如果依赖 PATH，上手与排障成本都会很高
- provider 行为对版本比较敏感，绑定一组经过测试的 `cloudflared` 版本更稳

### 7.2 内嵌后解包布局

建议不要把 `cloudflared` 作为“系统依赖”处理。当前实现更适合收敛为：

- 构建时把目标平台的 `cloudflared` 内嵌到 `codex-remote`
- 首次需要 `trycloudflare` 时，再把它解到当前进程旁边
- 如果目标位置已经有 `cloudflared`，则直接复用

运行时落盘后的形态仍然是：

```text
<versionsRoot>/<slot>/
  codex-remote
  cloudflared
```

好处：

- 对 systemd / launchd / Windows service 的 PATH 没有依赖
- 当前版本槽位切换时，`codex-remote` 和解出的 `cloudflared` 仍然一起切
- 回滚时 `cloudflared` 也天然跟着回滚
- 不需要为 provider 单独做另一套版本状态机

### 7.3 解析顺序

运行时解析 `cloudflared` binary 的顺序建议为：

1. `config.externalAccess.provider.tryCloudflare.binaryPath`
2. 当前版本槽位或当前进程旁边已存在的 `cloudflared`
3. 从主二进制内嵌资源解出的 `cloudflared`
4. PATH 中的 `cloudflared` 作为开发环境兜底

phase 1 不建议只保留 PATH 方案。

### 7.4 sidecar 元数据

如果后续需要把 sidecar 元数据纳入 `install-state.json`，不要单独再加 `CloudflaredBinaryPath` 这种 feature-specific 字段。建议直接抽象成通用结构：

```go
type BundledToolState struct {
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

type InstallState struct {
	// ...
	BundledTools map[string]BundledToolState `json:"bundledTools,omitempty"`
}
```

phase 1 如果实现成本太高，也可以先只在 runtime 内部解析，不急着第一天就写进 state。

## 8. 内部 Go 接口

### 8.1 daemon 对外总入口

建议新建 `internal/externalaccess`，由 daemon 持有一个 service：

```go
package externalaccess

type Purpose string

const (
	PurposePreview Purpose = "preview"
	PurposeReview  Purpose = "review"
	PurposeDebug   Purpose = "debug"
)

type IssueRequest struct {
	Purpose         Purpose
	TargetURL       string
	TargetBasePath  string
	LinkTTL         time.Duration
	SessionTTL      time.Duration
	AllowWebsocket  bool
}

type IssuedURL struct {
	ExternalURL  string
	ProviderKind string
	ExpiresAt    time.Time
}

type Service interface {
	IssueURL(ctx context.Context, req IssueRequest) (IssuedURL, error)
	Snapshot() Status
	Close() error
}
```

其中：

- `TargetURL` 是用户真正想打开的本地入口 URL
- `TargetBasePath` 用于限制同一 grant 允许代理的上游 path 前缀
- 如果 `TargetBasePath` 为空，默认按 `TargetURL` 推导一个“可加载资产但不无限放大”的前缀

### 8.2 provider 接口

```go
type Provider interface {
	Kind() string
	EnsurePublicBase(ctx context.Context, localListenerURL string) (PublicBase, error)
	Snapshot() ProviderStatus
	Close() error
}

type PublicBase struct {
	BaseURL   string
	StartedAt time.Time
}
```

phase 1 只实现 `trycloudflare`，但 service 必须通过接口依赖它，而不是在 handler 里直接拼 `cloudflared` 命令。

### 8.3 grant / session 管理

```go
type GrantSpec struct {
	Purpose        Purpose
	TargetURL      *url.URL
	TargetBasePath string
	AllowWebsocket bool
	ExpiresAt      time.Time
	SessionTTL     time.Duration
}

type GrantStore interface {
	Issue(spec GrantSpec) (Grant, string, error) // returns grant + raw exchange token
	Exchange(grantID string, token string) (GrantSession, error)
	Authorize(grantID string, sessionValue string) (Grant, error)
	Revoke(grantID string)
	Snapshot() GrantStatus
}
```

这里不要复用当前 `adminauth.Manager` 的“单 setup token”状态机，因为 preview/review 场景天然会同时存在多条 grant。更合理的做法是：

- 保持 `adminauth` 继续只管 setup/admin
- 在 `externalaccess` 内部引入自己的多 grant in-memory store
- 如果后面需要复用 HMAC session 编码能力，再做小范围共用抽象

## 9. HTTP surface 设计

### 9.1 独立 listener

独立 listener 只服务“外部授权访问”，不承载 admin/setup 页面：

- 监听地址：`127.0.0.1:<externalAccess.listenPort>`
- path 前缀：`/g/{grantID}/...`

这样可以避免把 admin scope、setup scope、preview scope 混成一套授权等级。

### 9.2 本地调试 API

为了让这套能力可观测、可手工验证，建议新增本地 admin-only API：

- `GET /api/admin/external-access/status`
- `POST /api/admin/external-access/link`

`POST /api/admin/external-access/link` 只用于本地调试和验证，consumer 正式接入还是应该直接调用 daemon 内部 service。

`GET /api/admin/external-access/status` 至少还应该投影：

- 当前 listener 是否存在
- 当前 provider/public base 是否存在
- 最近一次入站流量时间
- 最近一次出站流量时间
- 距离 idle auto-destroy 还剩多久

### 9.3 外部 URL 形态

建议签发结果固定为：

```text
https://<public-base>/g/<grant-id>/?t=<exchange-token>
```

第一次访问：

1. 校验 `grant-id + token`
2. 生成一个 path-scoped cookie
3. 302/303 跳转到干净 URL

跳转后：

```text
https://<public-base>/g/<grant-id>/
```

这样 token 不会长期留在地址栏、浏览器历史或后续 subresource request 里。

这里再补一条 product-safe 约束：

- `?t=` 虽然只用于第一次交换 cookie，但**不能**设计成“任何匿名 GET 第一次命中就永久失效”的一次性口令。

原因是：

- 飞书消息链路可能存在平台预取
- 浏览器、系统分享面板、安全扫描器也可能提前访问链接
- 第一个 consumer `#112 /debug admin` 需要在这类预取存在时仍然可用

因此 phase 1 的 safest default 应该是：

- `?t=` 在 `link TTL` 内是一个**可重复交换 session 的短时 bearer exchange token**
- 它的风险边界靠 `link TTL`、`grant allowlist`、`purpose` 和 path-scoped session 控制
- 不靠“第一次谁先 GET 到谁就永久抢走访问机会”来保证安全

## 10. 认证与 cookie 规则

### 10.1 grant token

grant token 的职责只有一个：

- 交换出一个 grant-scoped session cookie

不让 token 直接参与每一次请求鉴权，这样可以：

- 避免 token 挂在后续资源 URL 上
- 减少 Referer 泄漏
- 让 consumer 页面里的相对路径资源加载更自然

但这里要明确，phase 1 的 grant token 语义不是“单次消费码”，而是“短时 exchange token”：

- 在 `link TTL` 内，重复命中 `?t=` 应该仍能换出 grant-scoped session
- 这样即使飞书或中间链路发生预取，也不会把真实用户点击的机会提前烧掉
- 真正的收口点放在：
  - token 过期
  - grant allowlist
  - session TTL
  - 后续如有需要，再追加更强的 interactive confirmation / device binding

phase 1 不建议在没有更多运行时证据前就把 token 做成严格一次性消费。

### 10.2 session cookie

建议 cookie 规则：

- `HttpOnly`
- `Secure`
- `SameSite=Lax`
- `Path=/g/<grant-id>/`
- 只包含最小 claims：
  - `grantID`
  - `purpose`
  - `exp`

### 10.3 TTL 语义

建议分两层：

- `link TTL`
  - 外部授权 URL 最长多久还能交换出 cookie
- `session TTL`
  - 一旦 cookie 已建立，还能继续访问多久

默认：

- `link TTL = 10m`
- `session TTL = 30m`

这比“只有 URL TTL”更适合页面加载和后续交互。

## 11. 反代 allowlist 规则

### 11.1 默认拒绝

这一层不能成为 open proxy。默认规则必须是：

- 不接受 `target=` 之类的外部目标参数
- 不允许运行时切换到 grant 之外的 host/port
- 不允许代理非 loopback 目标

### 11.2 目标约束

只允许：

- `http://localhost:...`
- `http://127.0.0.1:...`
- `http://[::1]:...`
- `https://localhost/...` 这类本机目标

不允许：

- 远端 IP
- 局域网 IP
- Unix socket
- 任意 TCP CONNECT

### 11.3 path 前缀约束

grant 必须冻结：

- `target origin`
- `target base path`

只有落在同一 `origin + basePath` 内的请求才允许透传。

如果 upstream 重定向到了另一个 localhost 路径：

- 落在 allowlist 内：重写成同一 external grant path
- 不在 allowlist 内：拒绝，不跟随

## 12. 反代重写规则

### 12.1 必做重写

phase 1 的 proxy 不仅要“能转发”，还要能支撑真实网页加载，因此至少要做：

- `Location` header 重写
- `Set-Cookie Path` 重写
- 去掉 `Set-Cookie Domain`
- 正确设置 `X-Forwarded-Host` / `X-Forwarded-Proto` / `X-Forwarded-Prefix`

这里要明确 phase 1 的边界：

- 这层代理**不承诺**对 HTML / CSS / JS 正文做全量字符串重写
- 也不承诺把页面里写死的根绝对路径（例如 `/assets/...`、`/api/...`）自动改造成 grant 前缀路径

因此 phase 1 默认只保证下面两类页面可靠：

- 本身就是 prefix-aware 的页面
- 资源请求天然落在 `TargetBasePath` allowlist 内的页面

如果某个 consumer 页面严重依赖根绝对路径，而又不能通过自身配置适配 `X-Forwarded-Prefix` / base path，那么它不应该被视为“无需改动即可接到 phase 1 基座上”。

### 12.2 WebSocket 与 SSE

proxy 层应该支持 WebSocket 透传，因为后续交互页面可能会用到。

但是 phase 1 不能承诺 SSE，因为 Quick Tunnel 官方文档明确写了不支持 SSE。也就是说：

- 代理层支持 HTTP / WebSocket
- consumer 第一阶段不要依赖 SSE

### 12.3 idle auto-destroy

这条基座不是常驻公网入口，默认应在无流量时自动销毁。

建议规则：

- 以 listener 观察到的**入站或出站流量**为活跃信号
- 只要任一方向出现流量，就刷新 `LastActivityAt`
- 若连续 `30m` 没有任何入站或出站流量，则自动：
  - 关闭 provider
  - 关闭/回收 external-access listener runtime
  - 清掉当前 public base 状态
  - 让后续新的 `IssueURL()` 再次懒启动

这里的“出站流量”不是只看是否签发过 URL，而是看真实代理转发链路上是否向 upstream 发出了请求或 websocket 数据。

这样可以避免：

- 出门临时开过一次 `/debug admin` 后，把 tunnel 长时间挂在公网
- 没人使用时仍然持续占用 child process、随机域名和本地监听资源

## 13. trycloudflare provider 运行模型

### 13.1 启动方式

provider 启动 `cloudflared` 的推荐命令形态：

```bash
cloudflared tunnel \
  --url http://127.0.0.1:<external-access-port> \
  --no-autoupdate \
  --metrics 127.0.0.1:<metrics-port>
```

### 13.2 健康判定

只有同时满足下面条件，provider 才算 ready：

1. 解析到了 `https://*.trycloudflare.com` 公网基址
2. `cloudflared` 子进程仍然活着
3. metrics `/ready` 返回 `200`

### 13.2.1 idle 回收语义

即使 provider 仍然 `ready`，只要 external-access runtime 连续 `30m` 没有观察到入站或出站流量，也应主动回收，而不是保持常驻。

这意味着：

- `ready` 只表示“当前可用”，不表示“应该一直常驻”
- provider 生命周期除了异常退出/显式关闭外，还要支持 idle timeout 驱动的正常回收
- 后续新的 `IssueURL()` 或新的实际访问，再重新懒启动即可

### 13.3 配置隔离

因为 Quick Tunnel 官方文档明确提到 `.cloudflared/config.yaml` 会影响支持性，所以子进程必须隔离自己的配置目录。

建议策略：

- 为 child process 设置专用 `HOME` / `XDG_CONFIG_HOME` / Windows 对应用户配置目录
- 不复用用户真实主目录下的 `.cloudflared`

这是 phase 1 必须做的，不是优化项。

## 14. daemon 内部集成点

建议落点：

- `internal/externalaccess/*`
  - 核心 service、grant store、provider 接口、trycloudflare 实现
- `internal/app/daemon/external_access_http.go`
  - public listener handler
- `internal/app/daemon/startup.go`
  - 打印本地 external access listener 信息
- `internal/config/configfile.go`
  - 新增 `externalAccess` schema
- `internal/config/envfile.go`
  - 新增 env override 读取
- `internal/app/install/*`
  - sidecar 解析与未来 bundle metadata

daemon 侧建议新增：

```go
func (a *App) IssueExternalAccessURL(ctx context.Context, req externalaccess.IssueRequest) (externalaccess.IssuedURL, error)
```

consumer 统一走这条入口，不直接碰 provider。

## 15. 分阶段实现建议

### 阶段 1

- 配置结构
- 独立 listener
- grant store
- token -> cookie -> reverse proxy 主链路
- admin debug API

完成后应能在不接任何具体 consumer 的情况下手工验证：

- 给一个 localhost URL 生成短时外链
- 外链能打开
- 过期会拒绝

### 阶段 2

- `trycloudflare` provider
- bundled `cloudflared` 解析
- metrics `/ready` 健康检查
- provider 状态观测、idle auto-destroy 与自动重启策略

完成后应能做到：

- 内部 `IssueURL()` 在 tunnel 未启动时可懒启动
- 成功拿到 `trycloudflare` 外链
- 连续 `30m` 无入站/出站流量后自动销毁 tunnel，再次使用时可重新拉起

### 阶段 3

- 第一个真实 consumer 接入
- 补 redirect/cookie/websocket 边界测试
- 视需要再决定是否把某个 preview/review 页面正式挂上

当前已知优先顺序建议固定为：

1. 先接 `#112 /debug admin`
2. 验证飞书返回链接、cookie 建立、admin 页面加载与基础操作
3. 再评估是否值得把 preview/review 类页面接到同一基座

不要在第一个 consumer 还没跑通之前，把 stage 3 扩散成多个页面并行接入。

## 16. 需要测试的内容

至少需要覆盖：

- grant 生成、过期、cookie 交换
- loopback allowlist
- path 前缀越权拒绝
- `Location` / `Set-Cookie` 重写
- provider ready / not ready / exited
- `trycloudflare` URL 解析
- metrics `/ready` 健康检查
- child process 配置目录隔离
- `?t=` 在重复 GET / 预取场景下不会让真实用户首次点击直接失效
- 连续 `30m` 无入站/出站流量后会自动销毁 listener/provider，并且后续可重新懒启动

## 17. 当前建议

基于当前需求，建议把 `#83` 从“继续调研”切换成“可开工的 staged implementation issue”。

原因不是问题已经全部实现，而是：

- provider 选型已经收敛为 `trycloudflare`
- listener 与 auth model 已经明确
- 内部 builder 接口已经明确
- sidecar bundling 方向已经明确

下一步不应该继续停留在“需要更多方向”，而应该直接进入分阶段编码。

## 18. 参考资料

- Cloudflare Quick Tunnels
  - https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/
- Cloudflare Tunnel setup / quick tunnel summary
  - https://developers.cloudflare.com/tunnel/setup/
- Cloudflare Tunnel monitoring
  - https://developers.cloudflare.com/tunnel/monitoring/
- Cloudflare Kubernetes example mentioning `/ready`
  - https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/deployment-guides/kubernetes/
- cloudflared GitHub repository
  - https://github.com/cloudflare/cloudflared
- Gradio sharing guide
  - https://www.gradio.app/guides/sharing-your-app/
