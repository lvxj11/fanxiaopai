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

首次启动会在**当前工作目录**下生成 `data/config.ini` 和 SQLite 数据库，并导入内置菜品。
所以要在 `dist/` 里运行：这样 `dist/data/` 承接运行时文件，根目录只留源码。

用 Docker：

```bash
docker compose up -d   # 容器内 WORKDIR 是 /app，数据落在挂载的 ./data/
```

> Docker 不受上面的 `dist/` 约定影响：镜像里固定用 `/app/data`，由 `docker-compose.yml` 挂到宿主的 `./data/`。

## 配置

优先级：环境变量 > `data/config.ini` > 默认值（逐项覆盖）。

| 配置项 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `web_port` | `WEB_PORT` | `8080` | Web 端口 |
| `log_level` | `LOG_LEVEL` | `warn` | debug / info / warn / error |
| `db_path` | `DB_PATH` | `./data/menu.db` | SQLite 路径 |

## 怎么用

1. **菜库**：先把内置菜里不会做的删掉，再把自家常吃的加进来。菜越多，排出来的菜单越好看。
2. **设置**：选好每天排哪些餐（默认工作日早晚、周末三餐），勾上忌口（花生、海鲜、辛辣…）。
3. **菜单**：点「生成本周」。不满意就点某道菜上的 ↻ 只换这一格，或者点菜名换指定的菜；已经定了的格子点 🔒 锁住，重排时不会动。
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

## 目录结构

```
main.go                启动流程
utils/                 配置与日志
database/
  database.go          建表、菜品 CRUD、反馈、设置
  menu.go              菜单与采购清单
  seed.go              内置菜品导入（不覆盖用户数据）
  seed/dishes.json     122 道内置菜
generator/
  generator.go         带约束的加权随机 + 回溯 + 5 级降级
ai/provider.go         AI 换新鲜的预留接口（v1 未接入）
web/
  web.go               路由、页面渲染、gzip
  api_menu.go          菜单 / 采购 API
  api_dishes.go        菜品 / 反馈 API
  api_settings.go      排餐与忌口配置 API
  templates/           Go html/template + Alpine.js
  css/input.css        Tailwind + daisyUI 入口
  static/app.css       预编译产物（提交进仓库）
```

## 技术栈

Go 标准库 `net/http` + `html/template` + `//go:embed`，[modernc.org/sqlite](https://modernc.org/sqlite)（纯 Go，无需 cgo），[Alpine.js](https://alpinejs.dev/) 本地内置，Tailwind CSS v4 + daisyUI v5 预编译成静态 CSS。**没有前端构建链，没有 CDN 依赖，断网也能显示。**

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

## AI 换新鲜

`generator.Generator` 和 `ai.Provider` 是两个预留的接口：目前只有本地加权随机一个实现，`ai.NoopProvider` 表示未接入。接真实模型时新增一个实现即可，调用方和页面都不用改。

## 数据说明

内置菜品只在「菜库为空」（首次启动）时全量导入，之后**一条都不写**——你删过的内置菜、改过的做法/备料重启都不会被动。想找回来去设置页点「补回内置菜品」，只会补你库里没有的，不会覆盖你改过的菜。
