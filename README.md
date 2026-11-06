# 饭小排 · 家常饭排班表

给孩子做饭，每天不知道做什么菜。这个应用按你自己的菜库自动排出一周菜单，顺手生成采购清单。

- **菜库是你自己的**：内置 122 道常见家常菜，随时增删改。生成的菜一定是你会的。
- **排班不是抽签**：早餐只从快手池选、荤菜主料 48 小时内不重复、每天有绿叶菜、每周鱼虾至少两次、周末才排慢炖菜。
- **越用越准**：吃过打分，👎 直接拉黑，以后不再排；👍 的菜以后更容易被选中。
- **一键采购清单**：整周食材自动汇总去重、按分类分组，可勾选、可复制成文本发微信。

## 快速开始

```bash
go build -o dist/fanxiaopai .
cd dist && ./fanxiaopai
# 浏览器打开 http://localhost:8080
```

首次启动会在**当前工作目录**下生成 `config/config.ini`、`data/menu.db` 和日志。
**第一次打开会进入 `/setup` 设置页**：设管理员用户名、密码，并可勾选是否导入 122 道内置菜（默认勾选）。设置完成后自动登录；之后访问走 `/login`。

所以要在 `dist/` 里运行：`dist/config/`、`dist/data/` 承接运行时文件，根目录只留源码。

用 Docker：

```bash
docker compose up -d   # 容器内 WORKDIR 是 /app，配置/数据分别挂到 ./config、./data
```

> Docker 镜像里固定用 `/app/config`、`/app/data`，由 `docker-compose.yml` 挂到宿主目录；非 root（UID 1000）运行，两个目录都要可写。

## 鉴权

- **首次设置**：`/setup` 设用户名和密码，可勾选导入内置菜。凭据（PBKDF2 派生 + 随机盐 + 会话签名密钥）作为一个整体存在数据库的 `settings` 表，不建独立用户表。
- **登录**：Cookie 会话 `fxp_session`，HMAC 签名、自带过期时间，`HttpOnly` + `SameSite=Lax`；配了公网域名时自动加 `Secure`。
- **退出**：右上角 🚪 按钮，清会话 Cookie 并通知 Service Worker 清掉缓存里的私有数据。
- **公网部署**：项目默认监听 `0.0.0.0`，放到公网前务必设好账号密码。`domain` 配好后会启用 `Secure` Cookie 并把 `https://<域名>` 加入来源白名单。变更类 `/api/*` 额外要求 `application/json` 内容类型 + 同源来源（curl 等无 Origin 的脚本放行）。
- 登录流程里留了**二次验证接缝**（`web/auth.go` 的 `apiLogin`），方式未定，暂不实现，将来选定 TOTP / Passkey 等后在此插入校验即可，无需改数据库或路由。

## 配置

运行时配置在**独立目录** `config/config.ini`，与数据目录 `data/` 分开。仓库里跟踪的是模板 `config/config.ini.example`，生成的 `config/*.ini` 已加入 `.gitignore`（含鉴权设置，不入库、不进镜像）。

优先级：环境变量 > `config/config.ini` > 默认值（逐项覆盖）。

| 配置项 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `web_host` | `WEB_HOST` | `0.0.0.0` | 监听地址，`127.0.0.1` 仅本机 |
| `web_port` | `WEB_PORT` | `8080` | Web 端口 |
| `domain` | `DOMAIN` | *(空)* | 公网域名（不含协议）；填了则 Cookie `Secure`、加入来源白名单 |
| `session_ttl` | `SESSION_TTL` | `168` | 登录会话有效期（小时） |
| `log_level` | `LOG_LEVEL` | `warn` | debug / info / warn / error |
| `db_path` | `DB_PATH` | `./data/menu.db` | SQLite 路径 |
| *(目录)* | `CONFIG_DIR` | `./config` | 配置文件所在目录（仅影响路径，不是 ini 键） |

.env 见 `.env.example`。以下配置项暂未实现，等有需要再加：日志路径/切割、HTTP 超时、登录失败限流、密码最小长度、`base_url` 路径前缀、`cookie_secure` 覆盖、可信代理、SQLite `busy_timeout`、备份目录、会话 Cookie 名。

## 怎么用

1. **菜库**：先把内置菜里不会做的删掉，再把自家常吃的加进来。菜越多，排出来的菜单越好看。
2. **设置**：选好每天排哪些餐（默认工作日早晚、周末三餐），勾上忌口（花生、海鲜、辛辣…）。
3. **菜单**：点「生成本周」。不满意就点某道菜上的 ↻ 只换这一格，或者点菜名换指定的菜；已经定了的格子点 🔒 锁住，重排时不会动。编辑态里可「全部锁定」后只解锁少数几道再重排。
4. **采购**：按清单买，买一样勾一样（勾选会存下来），或者复制成文本发家人。

菜库太小导致排不出理想菜单时，页面会明确提示放宽了哪些约束，不会因为约束互相矛盾而卡住。

## 离线查看

在超市或厨房时手机不一定连得上家里的服务器，两条路都能离线看**已生成**的菜单和采购清单：

**1. 导出快照（随时可用，不需要额外配置）**

菜单页和采购页都有「导出离线快照」，点开的是一个自包含页面 —— 样式和数据全部内联，没有一行 JS、没有任何外部引用。用手机浏览器的「另存 / 下载页面」存下来，断网也能打开；采购清单可以现场勾选打钩（仅本次打开有效，不回写服务器）。

也可以直接 `GET /export?week=2026-11-02`，加 `?download=1` 会作为附件下载。

**2. PWA（需要 HTTPS）**

装到主屏幕后，断网打开看到的缓存数据与快照内容一样，区别是不用每次记得导出。

Service Worker 只能在 HTTPS（或 localhost）下注册，局域网 `http://内网IP` 不是 secure context，浏览器会自动跳过，**不影响现有用法**。用公网域名走 DNS-01 给内网域名签一张 Let's Encrypt 证书即可生效（证书只解析到内网，服务不用暴露到公网）；端口不限，443 或 8443 都是 secure context。应用本身不改，继续监听 8080，前面挂一个反向代理就行。

反代配好之后无需再改代码，离线能力自动生效。改了 `web/css/input.css` 并重新 `npm run build:css` 之后，记得把 `web/static/sw.js` 里的 `CACHE` 版本号 +1，否则用户看到的还是旧样式。

> 鉴权后的 SW 只缓存静态资源和「已登录时 200」的菜单/采购 JSON，绝不缓存登录/设置页、登录重定向或 401/403 响应；退出登录会清空缓存。

## 迁移

数据库结构版本用 SQLite `PRAGMA user_version` 记录，迁移步骤集中在 `database/migrate.go` 的有序列表里。

**开发阶段变动剧烈，迁移会被阶段性清理**（比如之前删过一次迁移函数）：需要加列时追加步骤并抬高版本；清理时清空步骤、把 baseline 重置，并在下面记一条不兼容变更。**版本基本稳定之前，这里只保留不兼容变更记录，不写详细的迁移/兼容文档**，避免反复改写浪费精力。

### 不兼容变更记录

| 阶段 | 变更 | 处理 |
|---|---|---|
| 未发布 / dev | 配置路径 `data/config.ini` → `config/config.ini` | 旧 `data/config.ini` 忽略，删掉即可 |
| 未发布 / dev | 新增鉴权：未初始化跳 `/setup`，接口需会话 | 部署前设好账号密码 |
| 未发布 / dev | 缺 `recipe`/`prep` 列的旧库不再支持 | 删除 `data/menu.db` 后重启重新初始化 |
| 未发布 / dev | 移除 AI 占位接口 `/api/ai/suggest` 与设置页卡片 | 无 |
| 未发布 / dev | 内置菜导入改由 `/setup` 选择，启动不再自动导入 | 空菜库可在设置页「补回内置菜品」 |

## 目录结构

```
main.go                启动流程
config/
  config.ini.example    配置模板（跟踪）
  config.ini            运行时生成（忽略）
utils/                 配置与日志
database/
  database.go          建表、菜品 CRUD、反馈、设置
  migrate.go          结构版本与有序迁移步骤
  menu.go             菜单与采购清单
  seed.go             内置菜品导入（首次 /setup 或「补回」时调用，不覆盖用户数据）
  seed/dishes.json    122 道内置菜
generator/
  generator.go        带约束的加权随机 + 回溯 + 5 级降级
web/
  web.go              路由、页面渲染、gzip
  auth.go             鉴权、会话、首次设置
  api_menu.go         菜单 / 采购 API
  api_dishes.go       菜品 / 反馈 API
  api_settings.go     排餐与忌口配置 API
  export.go           自包含离线快照
  templates/          Go html/template + Alpine.js（含独立 login/setup 页）
  css/input.css       Tailwind + daisyUI 入口
  static/app.css      预编译产物（提交进仓库）
  static/sw.js        Service Worker
```

## 技术栈

Go 标准库 `net/http` + `html/template` + `//go:embed`，[modernc.org/sqlite](https://modernc.org/sqlite)（纯 Go，无需 cgo），[Alpine.js](https://alpinejs.dev/) 本地内置，Tailwind CSS v4 + daisyUI v5 预编译成静态 CSS。鉴权只用标准库 `crypto/pbkdf2`、`crypto/hmac`。**没有前端构建链，没有 CDN 依赖，断网也能显示。**

改样式需要重新编译（产物要提交）：

```bash
npm install
npm run build:css
```

改图标同理，`static/icon-*.png` 是从 `favicon.svg` 渲染出来的产物，也要提交：

```bash
rsvg-convert -w 180 -h 180 -b white -o web/static/icon-180.png web/static/favicon.svg
rsvg-convert -w 192 -h 192 -b white -o web/static/icon-192.png web/static/favicon.svg
rsvg-convert -w 512 -h 512 -b white -o web/static/icon-512.png web/static/favicon.svg
```

## 数据说明

内置菜品**不再由启动自动导入**：首次 `/setup` 里可勾选导入；跳过则菜库为空，页面会引导去菜库添加或在设置页「补回内置菜品」。导入只在「菜库为空」时全量写入，之后一条都不写——你删过的内置菜、改过的做法/备料都不会被动；「补回内置菜品」只补库里没有的，不覆盖你改过的菜。
