# China Genshin University

CGU（China Genshin University，原神大学）是一套使用 Go `net/http` 构建的校园网站，包含公开首页、登录入口、学生教务门户和管理员后台。Go 服务编译为单个可部署二进制，MySQL 是可选的持久化存储。

前端静态资源位于 Go 项目惯用的 `web/` 目录；可通过 `CGU_STATIC_DIR` 或配置文件中的 `staticDir` 指向自定义目录。

这是一个非官方的同人校园项目，与 HoYoverse 或《原神》官方没有隶属关系。官网文案会引用并概括官方公开资讯，不代表官方立场。

## 本地运行

需要 Go 1.25+：

```powershell
go run .
```

打开 <http://127.0.0.1:8000/>。

## 首次启动与管理员

CGU 不包含公开的预置账号，也不会在浏览器端伪造登录。启动前必须在私有 `config.json` 或受保护的环境变量中设置唯一的引导管理员密码；密码至少 12 个字符，未配置时服务会拒绝监听端口。管理员用户名可通过 `adminUsername` 或 `CGU_ADMIN_USERNAME` 修改，默认用户名为 `admin`。

招生申请会写入 `cgu_admissions`（或内存存储），管理员后台只提供“同意录取”这一决策动作；同意后系统以幂等事务自动建立学生档案、生成学校邮箱、写入校内欢迎信，并尝试向申请人的联系邮箱发送不含初始密码的入学通知。学生门户使用 `studentEmailDomain` / `CGU_STUDENT_EMAIL_DOMAIN` 为已有学号的学生生成机构邮箱地址（默认 `@cgu.edu.kg`），并提供不依赖外部服务的校内收件箱。校内副本始终先保存，外发结果会记录为成功、失败、未配置或未知；超时、取消和服务重启后的未知状态必须由管理员确认后才可重试。初始密码只在首次审批响应中显示，服务端只保存 bcrypt 哈希；学生可以在个人资料页验证当前密码后轮换自己的密码，其他会话会被撤销。

示例配置（请复制为未纳入版本控制的 `config.json`，再填入自己的随机密码）：

```json
{
  "adminUsername": "admin",
  "adminPassword": "请替换为至少 12 个字符的随机密码"
}
```

每次启动都会将该配置同步为数据库中 `id=admin` 的引导管理员密码，便于轮换；历史版本创建的固定学生账号及其种子教务记录会在数据库迁移时删除。浏览器首次访问时会依据 `navigator.languages` 自动选择中文或英文，手动切换结果保存在浏览器本地。

### 世界观内容同步

截至 2026-08-24，首页与教务种子数据已按原神官方新闻页的最新公开条目同步到 7.0「无神怜爱的雪国」（Everwinter Without Mercy）与至冬（Snezhnaya）主题，并新增枫丹、纳塔、至冬三个研究方向。参考来源：[原神官方中文新闻](https://genshin.hoyoverse.com/zh-tw/news)、[原神官方英文新闻](https://genshin.hoyoverse.com/en/news)。CGU 页面中的课程与公告是虚构的学校内容，官方链接仅用于溯源。

### MySQL 持久化（可选）

默认使用内存数据。配置 `CGU_DB_ENABLED=true` 以及以下环境变量后，服务会自动创建表、写入课程和公告初始数据并在启动时加载。生产环境默认在 MySQL 不可用时拒绝启动；只有明确设置 `CGU_DB_ALLOW_MEMORY_FALLBACK=true`（或配置文件 `allowMemoryFallback: true`）才允许降级到内存模式，避免重启后悄然丢失教务数据：

```powershell
$env:CGU_DB_HOST = "127.0.0.1"
$env:CGU_DB_PORT = "3306"
$env:CGU_DB_USER = "cgu"
$env:CGU_DB_PASSWORD = "从环境变量注入"
$env:CGU_DB_NAME = "cgu"
$env:CGU_DB_ENABLED = "true"
$env:CGU_DB_ALLOW_MEMORY_FALLBACK = "false"
go run .
```

也可以设置 `CGU_DB_DSN`，或使用 `config.json` / `.env`（不要把真实密码提交到 Git）。配置优先级为进程环境变量 > `.env` > `config.json` > 默认值；模板见 [`config.example.json`](config.example.json) 和 [`.env.example`](.env.example)。`/healthz` 会返回 `storage: mysql`、`memory` 或 `memory-fallback`。

### 真实 SMTP 外发

SMTP 默认关闭。启用前请向邮件服务商申请专用 SMTP 凭据和发件地址，不要复用数据库密码或把密码写进 Git。生产环境推荐使用 587/STARTTLS 或 465/隐式 TLS；明文模式只有在显式设置 `allowInsecure` / `CGU_SMTP_ALLOW_INSECURE=true` 且使用内网测试中继时才允许。

```powershell
$env:CGU_SMTP_ENABLED = "true"
$env:CGU_SMTP_HOST = "smtp.example.com"
$env:CGU_SMTP_PORT = "587"
$env:CGU_SMTP_USERNAME = "cgu-notify@example.com"
$env:CGU_SMTP_PASSWORD = "从密钥管理器注入"
$env:CGU_SMTP_FROM = "cgu-notify@example.com"
$env:CGU_SMTP_FROM_NAME = "CGU Admissions"
$env:CGU_SMTP_AUTH = "auto"
$env:CGU_SMTP_TLS_MODE = "starttls"
$env:CGU_SMTP_TIMEOUT_SECONDS = "15"
go run .
```

配置文件对应 `smtp` 对象；字段见 [`config.example.json`](config.example.json)。支持的认证模式为 `auto`、`plain`、`login`、`cram-md5`、`scram-sha-256`、`xoauth2` 和 `none`。启用 SMTP 但主机、发件地址、认证或 TLS 参数不完整时，服务会在启动前失败并拒绝监听端口；生产外发同时要求健康的 MySQL 持久化，不能以 memory fallback 启动。邮件正文使用 UTF-8 纯文本 MIME 编码，地址和标题经过 CRLF 注入校验；应用不会记录 SMTP 密码。后台显示“已接受”只表示中继返回成功，不等同于最终收件箱投递。

SMTP 集成负责从 CGU 向联系邮箱发送通知和教务邮件；学校邮箱的校内收件箱始终可用。当前版本不伪造 POP3/IMAP 收件能力，若要接收外部来信，应在邮件服务商侧配置转发/Webhook 或单独的 IMAP 同步服务，再通过受控接口写入校内收件箱。

正式 HTTPS 部署请在私有 `config.json` 或环境中设置公网来源，并启用安全 Cookie：

```json
{
  "publicOrigin": "https://cgu.edu.kg",
  "cookieSecure": true
}
```

对应环境变量为 `CGU_PUBLIC_ORIGIN=https://cgu.edu.kg` 和 `CGU_COOKIE_SECURE=true`。`publicOrigin` 必须是没有路径、查询或片段的 `http://`/`https://` 来源；它用于 TLS 在反向代理终止时的登录和 CSRF 同源校验，反向代理部署必须配置此项。未配置时，服务只按 Go 监听器实际看到的 HTTP/HTTPS 方案接受当前 `Host`，且不会信任任意 `X-Forwarded-*` 请求头。若需要让登录/招生限流识别真实客户端地址，请在私有配置中设置 `CGU_TRUSTED_PROXIES`（逗号分隔的代理 IP/CIDR）；只有来自这些网段的连接才会读取 `X-Forwarded-For`，默认值为空。反向代理/WAF 仍应配置 TLS、请求体限制和边缘限流；安全审计记录见 [`security_best_practices_report.md`](security_best_practices_report.md)。

## 页面与接口

- `/`：CGU 招生首页，支持中文/英文切换和申请意向表单
- `/calendar`：公开校园日历，读取已发布的教务公告；页面提供可用的刷新状态
- `/login`：Cookie 会话登录（旧 `/login.html` 地址仍兼容）
- `/portal`：课程搜索、选课/退选、成绩、课表、公告、校内邮箱和个人资料（旧 `/portal.html` 地址仍兼容）
- `/admin`：课程、学生目录、成绩、课表、公告、招生、校内邮箱和全站内容编辑（旧 `/admin.html` 地址仍兼容）
- `/api/auth/*`：登录、退出和当前用户
- `/api/auth/password`：学生验证当前密码后轮换密码；成功后撤销该账号的其他会话
- `/api/courses`、`/api/enrollments`、`/api/grades`、`/api/schedule`、`/api/announcements`：教务数据
- `/api/catalog.csv`：公开课程目录下载，按 `lang=en` 或浏览器 `Accept-Language` 导出英文/中文主列
- `/api/admin/*`：管理员统计与 CRUD 接口；`/api/admin/admissions` 查看招生申请，使用 `POST /api/admin/admissions/{id}/approve` 完成唯一的“同意”动作。首次同意会在同一事务中创建学生档案、生成校内邮箱并只在响应中返回一次初始密码；重复请求只返回已批准状态，不会再次生成或暴露密码。
- `/api/admin/notifications`：管理员查看由招生申请生成的持久化通知，并通过 `PATCH /api/admin/notifications/{id}` 更新已读状态；写入 MySQL 时申请和通知使用同一事务，避免后台漏报
- `/api/admin/students`：管理员学生目录（GET 列表、POST 创建、PATCH/PUT 更新）；响应只返回脱敏资料和配置域名生成的学生邮箱，不返回密码或密码哈希
- `/api/admin/grades`、`/api/admin/schedule`：管理员维护成绩与课表（GET/POST/PATCH/PUT/DELETE），支持 `student_id`/`user_id` 筛选并校验学生、课程引用
- `/api/admin/admissions/{id}`：只允许编辑联系资料和内部备注；申请决定不能通过状态字段绕过唯一的“同意”动作，已自动建档的学号也不可被改写
- `/api/mailbox`：学生读取自己的校内收件箱并返回生成的学校邮箱地址；`PATCH /api/mailbox/{id}` 仅允许该收件人更新已读状态
- `/api/admin/mailbox`：管理员查看发送记录并向指定学生发送校内邮件；请求体带 `external: true` 时必须同时带客户端生成的 `idempotencyKey`，以防浏览器重试造成重复外发；审批通知会自动进入同一投递记录和重试机制；接口要求管理员会话和 CSRF 同源请求头
- `POST /api/admin/mailbox/{id}/retry`：重试失败或未配置的 SMTP 外发；正在发送的记录会拒绝并发重试，已成功投递的消息会拒绝重复发送。若结果未知，必须提交 `{"confirmUnknown":true}` 并由管理员确认中继未接受后才允许重试。
- `/api/admissions`：公开招生申请意向提交（服务端校验、持久化和按客户端限流）
- `/api/site-content`：公开读取当前语言内容覆盖
- `/api/admin/site-content`：管理员编辑全站双语文案、日期、数字、图片地址和链接

管理员后台的“网站内容”目录会汇总首页、登录页、学生门户、日历和管理页的所有 `data-i18n` 字段，并包含首页图片和外链资源。修改后刷新前台即可生效；启用 MySQL 后内容覆盖会持久化到 `cgu_site_content`。图片资源出于 CSP 安全限制使用本站或 `images.unsplash.com` 地址，链接支持 HTTPS、邮件地址、站内锚点和本站路径。课程与公告编辑器支持 `clearFields` 明确清空可选字段；招生备注支持 `clearNotes: true`，不会把旧值偷偷恢复。

未配置 MySQL 时课程、公告、招生申请、学生目录、校内邮件和教务修改保存在进程内存中，服务重启后会重新加载初始内容；启用 MySQL 后首次启动会创建 `cgu_*` 表（包括 `cgu_admissions` 和 `cgu_mailbox_messages`）并写入缺失的课程、公告和内容目录。已有旧版邮箱表、招生表和用户表会在启动时按列幂等迁移，保留原有数据。学生目录支持停用/启用账号：停用会立即撤销会话并阻止登录，但保留成绩、课表、邮箱和招生审计记录。SMTP 邮箱记录带唯一幂等键和数据库条件发送租约，支持单实例或共享 MySQL 的滚动重启；未配置数据库时只适合单进程运行。招生申请只在管理员接口返回，后台不提供状态下拉，必须使用唯一的 `POST /api/admin/admissions/{id}/approve` 同意动作；首次同意原子创建学生账号、校内邮箱和申请关联信息，并自动尝试外发入学通知，初始密码只响应一次。公开提交按客户端限流。学生账号由管理员创建或招生同意动作自动建立，服务端只保存密码哈希；学生可读取自己的成绩、课表和自己的校内邮件，管理员接口要求会话、管理员角色、同源请求头和 CSRF 防护。网站内容编辑器允许保存空的中英文值来恢复随版本发布的内置文案；公告支持中英文正文和草稿状态，招生申请支持只改备注而不改决定。后台和学生门户会定时刷新数据，单个接口失败时保留已成功加载的模块并显示可操作的部分失败提示。正式部署应使用 MySQL、HTTPS、反向代理、密钥管理和可观测的 SMTP 投递告警；多实例部署还应使用共享会话存储。

构建检查：

```powershell
go test ./...
go vet ./...
go build -o dist/cgu.exe .
```

发布或编辑 Release 时使用仓库内工具，正文会在提交前检查真实换行，并在 GitHub 接受后回读逐字验证，避免被错误编码成字面量 `\n`：

```powershell
./tools/New-CGURelease.ps1 -Tag v1.5.4 -NotesFile .\release-notes-v1.5.4.md
```
