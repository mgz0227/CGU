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

招生申请会写入 `cgu_admissions`（或内存存储），管理员可在后台查看并更新处理状态。学生门户使用 `studentEmailDomain` / `CGU_STUDENT_EMAIL_DOMAIN` 为已有学号的学生展示机构邮箱地址（默认 `@cgu.edu.kg`）；这只生成地址标识，不代表 SMTP 邮箱服务已经开通。

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

默认使用内存数据。配置 `CGU_DB_ENABLED=true` 以及以下环境变量后，服务会自动创建表、写入课程和公告初始数据并在启动时加载：

```powershell
$env:CGU_DB_HOST = "127.0.0.1"
$env:CGU_DB_PORT = "3306"
$env:CGU_DB_USER = "cgu"
$env:CGU_DB_PASSWORD = "从环境变量注入"
$env:CGU_DB_NAME = "cgu"
$env:CGU_DB_ENABLED = "true"
go run .
```

也可以设置 `CGU_DB_DSN`，或使用 `config.json` / `.env`（不要把真实密码提交到 Git）。配置优先级为进程环境变量 > `.env` > `config.json` > 默认值；模板见 [`config.example.json`](config.example.json) 和 [`.env.example`](.env.example)。`/healthz` 会返回 `storage: mysql`、`memory` 或 `memory-fallback`。

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
- `/login`：Cookie 会话登录（旧 `/login.html` 地址仍兼容）
- `/portal`：课程搜索、选课/退选、成绩、课表、公告和个人资料（旧 `/portal.html` 地址仍兼容）
- `/admin`：课程、公告、教务统计，以及全站内容管理（旧 `/admin.html` 地址仍兼容）
- `/admin`：课程、学生目录、成绩、课表、公告、招生和全站内容编辑
- `/api/auth/*`：登录、退出和当前用户
- `/api/courses`、`/api/enrollments`、`/api/grades`、`/api/schedule`、`/api/announcements`：教务数据
- `/api/admin/*`：管理员统计与 CRUD 接口；`/api/admin/admissions` 查看招生申请并更新处理状态
- `/api/admin/students`：管理员学生目录（GET 列表、POST 创建、PATCH/PUT 更新）；响应只返回脱敏资料和配置域名生成的学生邮箱，不返回密码或密码哈希
- `/api/admin/grades`、`/api/admin/schedule`：管理员维护成绩与课表（GET/POST/PATCH/PUT/DELETE），支持 `student_id`/`user_id` 筛选并校验学生、课程引用
- `/api/admissions`：公开招生申请意向提交（服务端校验、持久化和按客户端限流）
- `/api/site-content`：公开读取当前语言内容覆盖
- `/api/admin/site-content`：管理员编辑全站双语文案、日期、数字、图片地址和链接

管理员后台的“网站内容”目录会汇总首页、登录页、学生门户和管理页的所有 `data-i18n` 字段，并包含首页图片和外链资源。修改后刷新前台即可生效；启用 MySQL 后内容覆盖会持久化到 `cgu_site_content`。图片资源出于 CSP 安全限制使用本站或 `images.unsplash.com` 地址，链接支持 HTTPS、邮件地址和站内锚点。

未配置 MySQL 时课程、公告、招生申请、学生目录和教务修改保存在进程内存中，服务重启后会重新加载初始内容；启用 MySQL 后首次启动会创建 `cgu_*` 表（包括 `cgu_admissions`）并写入缺失的课程、公告和内容目录。招生申请只在管理员接口返回，支持 `pending`、`reviewing`、`contacted`、`accepted`、`rejected` 和 `withdrawn` 状态；公开提交按客户端限流。学生账号由管理员创建，服务端只保存密码哈希；学生可读取自己的成绩和课表，管理员接口要求会话、管理员角色、同源请求头和 CSRF 防护。正式部署应使用 MySQL、HTTPS、反向代理和密钥管理。

构建检查：

```powershell
go test ./...
go vet ./...
go build -o dist/cgu.exe .
```
