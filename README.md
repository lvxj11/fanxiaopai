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

## AI 换新鲜

`generator.Generator` 和 `ai.Provider` 是两个预留的接口：目前只有本地加权随机一个实现，`ai.NoopProvider` 表示未接入。接真实模型时新增一个实现即可，调用方和页面都不用改。

## 数据说明

内置菜品按「菜库为空」和「种子版本」判断是否导入：空表才全量导入，之后**一条都不写**——你删过的内置菜不会因为重启自己回来。想找回来去设置页点「补回内置菜品」，只会补你库里没有的，不会覆盖你改过的菜。
