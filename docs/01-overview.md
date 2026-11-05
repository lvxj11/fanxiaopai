# 01 项目概述

## 1. 项目定位

饭小排（fanxiaopai）是一个面向家庭的「一周菜单排班 + 采购清单」应用。它不是菜谱搜索引擎，而是围绕**用户自己的菜库**做约束排班：

- 菜库由用户维护（内置 122 道家常菜作为初始数据，可增删改）。
- 排班遵守一组生活化约束：早餐只选快手菜、荤菜主料 48 小时内不重复、每天有绿叶菜、每周鱼虾至少两次、周末才排耗时长的菜。
- 吃过之后可打分：`👎` 拉黑不再排，`👍` 提高出现概率。
- 一键汇总整周食材，按分类分组、可勾选、可复制文本。

仓库同时提供两种离线查看方式：服务端渲染的**自包含 HTML 快照**（无需 HTTPS），以及在 HTTPS 环境下的 **PWA / Service Worker 缓存**。

## 2. 功能清单

| 功能 | 页面 / 接口 | 说明 |
|---|---|---|
| 周菜单生成 | `/menu`、`POST /api/menu/generate` | 编辑态一个「重排未锁定」按钮：空菜单全量生成，有菜单时只换未锁定格 |
| 单格换菜 | `POST /api/menu/item/reroll` | 只重排指定格子，其他格子不动 |
| 指定换菜 | `PUT /api/menu/item/replace` | 用户从菜库中指定一道菜 |
| 锁定格子 | `POST /api/menu/item/lock` | 重排时跳过锁定格 |
| 全部锁定 / 解锁 | `POST /api/menu/lock-all` | 一次锁定或解锁整周，配合「重排未锁定」只换少数几道 |
| 复用历史菜单 | `POST /api/menu/copy` | 把某周菜单复制到目标周 |
| 菜品库管理 | `/dishes`、`/api/dishes*` | 增删改查、拉黑/恢复、补回内置菜 |
| 反馈打分 | `POST /api/dishes/{id}/feedback` | 0 不吃 / 1 一般 / 2 爱吃，联动权重 |
| 排餐配置 | `/settings`、`PUT /api/settings/meal-slots` | 每天排哪些餐 |
| 忌口配置 | `/settings`、`PUT /api/settings/avoid-tags` | 带标签的菜一律不排 |
| 采购清单 | `/shopping`、`/api/shopping` | 按分类聚合、合并数量、保存勾选状态 |
| 离线快照 | `GET /export` | 样式与数据全内联、无 JS，断网可看 |
| 健康检查 | `GET /api/health` | 返回 `status` 与 `version` |
| AI 换新鲜（预留） | `POST /api/ai/suggest` | 当前固定返回未配置 |

## 3. 技术栈

| 层次 | 选型 | 说明 |
|---|---|---|
| 语言 | Go 1.27 | 单一静态二进制 |
| HTTP | `net/http` | Go 1.22+ 的 `ServeMux`，路由在 [web/web.go](../web/web.go) 集中注册 |
| 模板 | `html/template` + `//go:embed` | 模板编译进二进制 |
| 数据库 | `modernc.org/sqlite` v1.61.0 | 纯 Go，无 cgo；`SetMaxOpenConns(1)` |
| 前端脚本 | Alpine.js（本地文件） | 无 CDN、无打包步骤 |
| 样式 | Tailwind CSS v4 + daisyUI v5 | 预编译为 [web/static/app.css](../web/static/app.css) 并提交 |
| 图标 | 预渲染 PNG + SVG favicon | 由 `favicon.svg` 离线渲染，产物提交 |
| 容器 | 多阶段 Dockerfile，Alpine 3.24 | 非 root 运行，数据卷 `/app/data` |

**明确没有的东西**：前端构建链、CDN 依赖、ORM、cgo、外部缓存/消息队列、多进程/多实例假设。

## 4. 目录结构

```text
.
├── main.go                    进程入口
├── go.mod / go.sum            Go 模块定义（module fanxiaopai）
├── package.json               仅用于预编译 CSS 的 npm 脚本
├── Dockerfile                 多阶段构建
├── docker-compose.yml         单服务编排，挂载 ./data
├── .env.example               环境变量示例
├── .github/workflows/
│   ├── ci.yml                 格式、vet、测试、构建、CSS 产物同步校验
│   └── docker.yml             tag 发版：三平台二进制 + Release + 多架构镜像
├── utils/
│   ├── config.go              ini + 环境变量配置加载
│   ├── logger.go              分级日志与按大小轮转
│   └── version.go             产品名与版本号
├── database/
│   ├── model.go               领域模型与枚举常量
│   ├── database.go            建表、菜品/反馈/设置 DAO
│   ├── menu.go                菜单、采购清单、勾选状态
│   ├── seed.go                内置菜导入与校验
│   └── seed/dishes.json       122 道内置菜
├── generator/
│   ├── generator.go           排班算法
│   └── generator_test.go      约束与降级行为测试
├── ai/
│   └── provider.go            Provider 接口 + NoopProvider
└── web/
    ├── web.go                 路由注册、页面渲染、通用工具、gzip
    ├── api_menu.go            菜单与采购 API
    ├── api_dishes.go          菜品、反馈、补回内置菜 API
    ├── api_settings.go        排餐与忌口 API
    ├── export.go              自包含离线快照
    ├── templates/             base.html + menu/dishes/shopping/settings/export
    ├── css/input.css          Tailwind + daisyUI 入口
    └── static/                预编译 CSS、Alpine、图标、manifest、sw.js
```

运行时产物（不在版本库中）：

```text
data/
├── config.ini                 首次启动生成
├── menu.db                    SQLite 主库（含 -wal/-shm 视使用情况）
└── fanxiaopai.log             日志（超过 10MB 轮转为 .old）
```

本地开发的约定是把二进制和运行时数据都放在 `dist/`，根目录只留源码（见 [08 构建与部署](08-build-run.md)）。

## 5. 关键设计原则

1. **零外部运行时依赖**：模板、CSS、Alpine 全部嵌入/本地提供，断网仍可显示；SQLite 用纯 Go 驱动，交叉编译不需要 cgo。
2. **约束必须可降级**：菜库再小也不能卡死。生成器按 5 级放宽策略依次尝试，最后一轮做尽力而为填充，并把放宽内容返回给页面。
3. **内置数据只初始化一次**：只有菜库为空时才全量导入种子；用户删过、改过的菜重启不会被动。需要时通过「补回内置菜品」显式补缺，且只补不覆盖。
4. **单进程、单连接**：家庭规模应用，SQLite 一连接足够，避免并发写入与锁复杂度。
5. **前后端同源、无鉴权**：面向家庭局域网使用；所有页面与 API 同进程同源，不设登录与权限体系。
6. **移动优先**：底部 dock 导航、44px 触控目标、单列到多列自适应布局、深浅色主题跟随系统并可手动切换。

## 6. 版本与命名

- 产品名：`饭小排`；副标题：`家常饭排班表`；版本常量 `0.3.3`。
- 启动参数 `-v` 输出版本，例如：

```bash
./fanxiaopai -v
# 饭小排 家常饭排班表 v0.3.3
```

- Go module 名为 `fanxiaopai`，包内导入路径为 `fanxiaopai/<包名>`。
