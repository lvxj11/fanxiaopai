# 饭小排 · Code Wiki

> 给孩子做饭，每天不知道做什么菜。饭小排按你自己的菜库自动排出一周菜单，并汇总成采购清单。

本 Wiki 面向需要阅读、修改或部署本项目的开发者，覆盖：整体架构、模块职责、关键类型与函数、数据库设计、排班算法、HTTP API、前端约定、构建与部署。

---

## 文档索引

| 文档 | 内容 |
|---|---|
| [01 项目概述](01-overview.md) | 项目定位、功能清单、技术栈、目录结构、设计原则 |
| [02 整体架构](02-architecture.md) | 分层架构、模块依赖关系、启动时序、核心数据流 |
| [03 数据模型](03-data-model.md) | SQLite 表结构、实体字段、枚举常量、JSON 列约定、种子数据 |
| [04 模块与函数](04-modules.md) | `utils` / `database` / `ai` / `web` 中关键类型与函数逐一说明 |
| [05 排班算法](05-generator.md) | 角色模型、硬/软约束、5 级降级、MRV 选格、加权抽样、已知实现细节 |
| [06 Web 层](06-web-layer.md) | 路由、模板渲染、前端约定、gzip、静态资源、离线快照、PWA |
| [07 API 参考](07-api-reference.md) | 全部 HTTP 接口的请求/响应/错误码与示例 |
| [08 构建与部署](08-build-run.md) | 本地构建运行、配置优先级、Docker、CI/CD、测试与排障 |

## 30 秒了解

| 项目 | 说明 |
|---|---|
| 语言 / 运行时 | Go 1.27，单进程，无 cgo |
| Web | 标准库 `net/http` + `html/template` + `//go:embed` |
| 数据库 | SQLite（`modernc.org/sqlite` 纯 Go 实现），单连接 |
| 前端 | Alpine.js 本地内置 + 预编译 Tailwind CSS v4 / daisyUI v5，无 CDN、无运行时构建 |
| 数据目录 | 当前工作目录下的 `data/`（配置、SQLite、日志） |
| 默认端口 | `8080` |
| 版本 | `0.3.3`（以 [utils/version.go](../utils/version.go) 或 `GET /api/health` 为准；本 Wiki 中的示例版本号会随发版更新） |

## 最快跑起来

```bash
go build -o dist/fanxiaopai .
cd dist && ./fanxiaopai
# 打开 http://localhost:8080
```

首次启动会在 `dist/data/` 生成 `config.ini`、`menu.db`，并导入内置的 122 道菜。

## 代码地图

```text
main.go                 进程入口：参数解析、加载配置、初始化日志与数据库、启动 Web
utils/                  配置（ini + 环境变量）、日志（分级 + 轮转）、版本常量
database/               SQLite 建表、菜品/菜单/采购/反馈/设置的 DAO、内置菜导入
generator/              排班算法（纯计算，只依赖 database 中的数据类型与常量）
ai/                     AI 菜谱 Provider 抽象（当前为 Noop 占位）
web/                    HTTP 路由、页面渲染、JSON API、离线快照、静态资源嵌入
  templates/            页面模板（base.html + 每个页面一个 content 模板）
  css/input.css         Tailwind/daisyUI 编译入口
  static/               app.css、alpine.min.js、PWA 图标、sw.js
database/seed/          内置菜品 JSON（通过 go:embed 打包）
```

## 推荐阅读路线

- 想了解「菜单是怎么排出来的」：先看 [02 整体架构](02-architecture.md) 的生成时序，再读 [05 排班算法](05-generator.md)。
- 想改接口或加页面：读 [06 Web 层](06-web-layer.md) 与 [07 API 参考](07-api-reference.md)。
- 想改数据库/字段：读 [03 数据模型](03-data-model.md) 与 [04 模块与函数](04-modules.md)。
- 想部署或发版：读 [08 构建与部署](08-build-run.md)。
