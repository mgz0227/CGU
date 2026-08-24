# China Genshin University

CGU（China Genshin University，原神大学）是一套使用 Go `net/http` 构建的校园网站，包含公开首页、登录入口、学生教务门户和管理员后台。Go 服务编译为单个可部署二进制，MySQL 是可选的持久化存储。

## 本地运行

需要 Go 1.24+：

```powershell
go run .
```

打开 <http://127.0.0.1:8000/>。

## 演示账号

| 角色 | 账号 | 默认密码 |
| --- | --- | --- |
| 学生 | `student` | `student-demo` |
| 教务管理员 | `admin` | `admin-demo` |

启动前可以用 `CGU_STUDENT_PASSWORD` 和 `CGU_ADMIN_PASSWORD` 环境变量覆盖默认密码；用 `CGU_ADDR=0.0.0.0:8000` 修改监听地址。浏览器首次访问时会依据 `navigator.languages` 自动选择中文或英文，手动切换结果保存在浏览器本地。

### MySQL 持久化（可选）

默认使用内存演示数据。配置 `CGU_DB_ENABLED=true` 以及以下环境变量后，服务会自动创建表、写入首批演示数据并在启动时加载：

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

## 页面与接口

- `/`：CGU 招生首页，支持中文/英文切换和申请意向表单
- `/login.html`：Cookie 会话登录
- `/portal.html`：课程搜索、选课/退选、成绩、课表、公告和个人资料
- `/admin.html`：课程与公告的新增、编辑、删除，以及教务统计
- `/api/auth/*`：登录、退出和当前用户
- `/api/courses`、`/api/enrollments`、`/api/grades`、`/api/schedule`、`/api/announcements`：教务数据
- `/api/admin/*`：管理员统计与 CRUD 接口

未配置 MySQL 时示例数据保存在进程内存中，服务重启后会恢复为演示数据；启用 MySQL 后首次启动会创建 `cgu_*` 表并写入缺失的种子数据。正式部署应使用 MySQL、HTTPS、反向代理和密钥管理。

构建检查：

```powershell
go test ./...
go vet ./...
go build -o dist/cgu.exe .
```
