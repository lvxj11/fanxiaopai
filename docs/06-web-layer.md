# 06 Web 层

实现文件：[web/web.go](../web/web.go)、[web/api_menu.go](../web/api_menu.go)、[web/api_dishes.go](../web/api_dishes.go)、[web/api_settings.go](../web/api_settings.go)、[web/export.go](../web/export.go)。

Web 层负责：路由注册、页面渲染、JSON API、静态资源、gzip、离线快照。业务规则不在这一层（排班在 `generator`，持久化在 `database`）。

---

## 1. 服务器装配

```go
type Server struct {
    config    *utils.Config
    db        *database.Database
    generator generator.Generator // 默认 generator.NewLocalWeighted(nil)
    ai        ai.Provider         // 默认 ai.NewNoopProvider()
}
```

`Handler()` 构建 `http.NewServeMux` 并返回 `gzipMiddleware(mux)`。`Start(port)` 调用 `http.ListenAndServe(":"+port, Handler())`。

启动后行为：

- 启动信息写日志并输出到控制台；
- 随后 `SetConsoleOutput(false)`，请求期日志只进文件；
- 监听失败时恢复控制台输出并记录错误。

监听地址是 `:端口`，即对局域网开放，没有鉴权。这是家庭内网使用的设计前提。

---

## 2. 路由表

### 2.1 页面与静态资源

| 方法 | 路径 | Handler | 说明 |
|---|---|---|---|
| GET | `/` | `redirectToMenu` | 302 到 `/menu`；其他未知路径返回 404 |
| GET | `/menu` | `showMenu` | 周菜单页 |
| GET | `/dishes` | `showDishes` | 菜品库页 |
| GET | `/shopping` | `showShopping` | 采购清单页 |
| GET | `/settings` | `showSettings` | 设置页 |
| GET | `/favicon.svg` | `serveFavicon` | SVG 图标，缓存 1 天 |
| GET | `/export` | `exportSnapshot` | 自包含离线快照 |
| ANY | `/static/` | `serveStatic` | 嵌入静态文件 |

页面 Handler 不显式限制方法；非 GET 请求同样会返回页面内容。

### 2.2 API

| 方法 | 路径 | Handler |
|---|---|---|
| GET | `/api/menu` | `apiMenu` |
| POST | `/api/menu/generate` | `apiGenerateMenu` |
| POST | `/api/menu/copy` | `apiCopyMenu` |
| POST | `/api/menu/item/reroll` | `apiRerollItem` |
| PUT | `/api/menu/item/replace` | `apiReplaceItem` |
| POST | `/api/menu/item/lock` | `apiLockItem` |
| POST | `/api/menu/lock-all` | `apiLockAll` |
| GET | `/api/shopping` | `apiShopping` |
| PUT | `/api/shopping/checked` | `apiShoppingChecked` |
| GET / POST | `/api/dishes` | `apiDishes` |
| PUT / DELETE | `/api/dishes/{id}` | `apiDish` |
| POST | `/api/dishes/{id}/blocked` | `apiDish` → `setBlocked` |
| POST | `/api/dishes/{id}/feedback` | `apiDish` → `addFeedback` |
| POST | `/api/dishes/restore-seed` | `apiDish` → `restoreSeed` |
| GET / PUT | `/api/settings/meal-slots` | `apiMealSlots` |
| GET / PUT | `/api/settings/avoid-tags` | `apiAvoidTags` |
| POST | `/api/ai/suggest` | `apiAISuggest`（当前 501） |
| GET | `/api/health` | `apiHealth` |

请求/响应细节见 [07 API 参考](07-api-reference.md)。

> 路由使用 Go 标准库 `ServeMux` 的**路径前缀**匹配，不用 `method pattern`。因此 `/api/dishes` 与 `/api/dishes/` 分别注册；后者内部再手动切分路径，支持 `/api/dishes/123`、`/api/dishes/123/blocked`、`/api/dishes/restore-seed`。

---

## 3. 页面渲染

### 3.1 模板结构

- [base.html](../web/templates/base.html)：完整 HTML 骨架，包含顶栏、底部 dock 导航、toast、主题脚本、API 封装、Service Worker 注册；正文位置是 `{{template "content" .}}`。
- `menu.html` / `dishes.html` / `shopping.html` / `settings.html`：各自 `{{define "content"}}...{{end}}`，并附带该页面的 Alpine 组件函数。
- [export.html](../web/templates/export.html)：独立完整文档，不继承 `base.html`。

`renderPage` 每次请求执行：

```go
template.ParseFS(templateFiles, "templates/base.html", "templates/"+contentTemplate)
```

即**每次请求重新解析**两个模板，不使用缓存。页面数量少、模板小，这样实现最简单，也避免缓存失效问题。

注入的模板数据：

| 字段 | 用途 |
|---|---|
| `Title` | `<title>` |
| `CurrentPage` | 高亮底部导航当前项 |
| `Version` | 页脚 |
| `Today` | 已注入但当前模板未使用 |

### 3.2 静态资源

`//go:embed static/*` 嵌入 [web/static](../web/static)。`serveStatic` 用 `http.FileServer(http.FS(staticFiles))` 提供服务，并做两个特例：

| 路径 | 处理 | 原因 |
|---|---|---|
| `/static/sw.js` | 设置 `Service-Worker-Allowed: /`、`Cache-Control: no-cache` | Service Worker 默认 scope 只有 `/static/`，需提升到根才能控制 `/menu` 等页面 |
| `/static/manifest.webmanifest` | 设置 `Content-Type: application/manifest+json` | `FileServer` 按扩展名推断，会误发 `text/plain` |

---

## 4. 前端约定

前端没有构建链，Alpine.js 是本地文件 [web/static/alpine.min.js](../web/static/alpine.min.js)，样式是预编译的 `app.css`。

### 4.1 `base.html` 提供的全局能力

| 能力 | 实现 |
|---|---|
| 主题切换 | `<head>` 中同步执行的脚本，读 `localStorage['menu-theme']`，否则跟随 `prefers-color-scheme`；写入 `document.documentElement.dataset.theme`；`__toggleTheme()` 手动切换 |
| API 封装 | `window.api(path, options)`：自动加 `Content-Type: application/json`，非 2xx 抛出后端 `error` 文案；`window.post` / `window.put` 是便捷包装 |
| 轻提示 | `window.toast(msg, kind)`：`kind==='error'` 用红色，否则绿色，2.2 秒后自动隐藏 |
| Service Worker | 仅在 `isSecureContext && 'serviceWorker' in navigator` 时注册 `/static/sw.js` |
| 主题闪避 | 主题脚本位于 `<head>` 且同步执行，避免先渲染浅色再切深色 |

### 4.2 页面组件

| 页面 | Alpine 组件 | 主要状态 |
|---|---|---|
| 菜单 | `menuApp()` | `week/weeks/menu/mealSlots/editing/warnings/picker/recipe` |
| 菜库 | `dishesApp()` | `dishes/filter/editor` |
| 采购 | `shoppingApp()` | `week/groups/checked/saveTimer` |
| 设置 | `settingsApp()` | `mealSlots/allTags/avoidTags` |

菜单页的「浏览 / 编辑」是前端双状态，不持久化，刷新回到浏览态。**锁定只在生成/重排时有意义，因此只在编辑态呈现**：

- 浏览态（只读）：点菜名看做法/备料，不显示任何锁定标记，也不显示任何会改数据的按钮；仅保留「导出离线快照」。
- 编辑态：每格三列等宽的「随机换 / 指定换 / 锁定」按钮，锁定格用行底色 + 实心按钮区分；整周只有一个「重排未锁定」按钮（始终 `keep_locked:true`，空菜单时等于全量生成），另有「全部锁定 / 全部解锁」用于「先全锁、再解锁不满意的几道来重排」。

### 4.3 几个容易踩坑的实现约定

1. **daisyUI 的 `modal-open` 必须动态挂**
   模板里写 `:class="picker.open ? 'modal-open' : ''"`。若写死 `modal-open`，`:root:has(.modal.modal-open)` 会给 `html` 加 `overflow:hidden`，即使弹窗 `display:none` 也会锁死整页滚动。

2. **剪贴板降级**
   局域网 `http://内网IP` 不是 secure context，`navigator.clipboard` 可能不存在。`copyToClipboard` 在不可用时回退到 `textarea + document.execCommand('copy')`。

3. **采购勾选节流**
   每次勾选后 400ms 防抖再 `PUT /api/shopping/checked`，避免高频点击产生大量写请求。

4. **搜索防抖**
   菜库搜索 `@input.debounce.300ms="load()"`。

5. **`?edit=<id>` 深链**
   菜单页做法弹窗为空时引导到 `/dishes?edit=<id>`，菜库页初始化时自动打开编辑器；若该菜被拉黑不在默认列表，会再请求 `?blocked=1`。

6. **周切换**
   `shift(n)` 用本地日期加减 7 天并写 `history.replaceState`，因此周参数始终是合法的 `YYYY-MM-DD`。

---

## 5. 通用请求处理约定

| 工具 | 行为 |
|---|---|
| `readJSON` | 请求体最大 1MB；解析失败由调用方返回 400「请求格式有误」 |
| `writeJSON` | 设置 `application/json; charset=utf-8` 与状态码 |
| `writeError` | 输出 `{"error": "中文提示"}` |
| `requirePOST` | 非 POST 返回 405「只支持 POST」 |
| `apiError` | 预期错误（`generator.ErrNoDishes`、`ai.ErrNotConfigured`）→ 400 + 原文；其他错误记日志 → 500 + fallback |
| `weekParam` | `?week=` 合法则用，否则取本周一 |
| `mondayOf` | `Weekday()` 减 1，周日（0）视为 6，得到周一日期 |
| `loadMenu` | 周格式非法 400；菜单不存在或为空 404「这一周还没有菜单，先生成一份吧」 |

### 5.1 gzip 中间件

```text
Accept-Encoding 含 gzip → 包 gzip.Writer
  设置 Content-Encoding: gzip
  设置 Vary: Accept-Encoding
  删除 Content-Length
  响应写入透传到 gzip.Writer
```

对所有路径生效（包括静态资源与 JSON）。没有最小长度阈值，极小的响应也会被压缩。

---

## 6. 离线快照 `/export`

目标：不依赖服务器、不依赖 HTTPS、不含 JS，存到手机上断网可打开。

处理流程（[web/export.go](../web/export.go)）：

1. 读取嵌入的 `static/app.css`；
2. `weekParam` 确定周；
3. 读取菜单；有菜单时再算采购清单与勾选状态；
4. `exportDays` 按 `meal_slots` 组装 7 天，日期由周一 + 天序号推算；
5. 渲染 `templates/export.html`，CSS 通过 `template.CSS` 内联进 `<style>`；
6. `?download=1` 时设置 `Content-Disposition`（ASCII 文件名 + `filename*=UTF-8''` 中文名）。

快照的勾选只是浏览器本地复选框状态，`checked` 只决定初始是否打勾；CSS `:checked ~ .snap-name` 提供删除线效果，不回写服务器。

---

## 7. PWA 与离线缓存

[web/static/sw.js](../web/static/sw.js)：

| 资源类型 | 策略 |
|---|---|
| `/api/menu`、`/api/shopping` | stale-while-revalidate：先给缓存，同时后台更新 |
| 页面导航（`request.mode === 'navigate'`） | network-first：有网走网络并写缓存，断网回落到缓存，最终回落到 `/menu` |
| `/static/*` | cache-first |
| 非 GET、跨源请求 | 不拦截 |

注意事项：

- 缓存名 `CACHE = 'fanxiaopai-v3'`。**修改 `web/static/app.css` 后必须把版本号 +1**，否则旧缓存不会失效。
- 只在 HTTPS 或 localhost 下注册；局域网 HTTP 下静默跳过，不影响普通使用。
- `install` 时预缓存 shell：`/menu`、`/shopping`、CSS、Alpine、manifest、三个图标。

---

## 8. 安全与边界

| 项 | 现状 |
|---|---|
| 鉴权 | 无。任何能访问端口的人都能读写菜库与菜单 |
| CSRF | 无防护，依赖内网环境假设 |
| 请求体大小 | 1MB 上限 |
| 输入校验 | 菜品字段在 `validateDish` 中校验；排餐配置在 `apiMealSlots` 中清洗；周日期严格校验 |
| SQL 注入 | 全部使用参数化查询；`json_each` 查询亦为固定 SQL |
| XSS | 页面模板使用 `html/template` 自动转义；快照同样使用 `html/template`；Alpine 的 `x-text` 不解析 HTML |
| 日志敏感信息 | 不记录请求体；日志内容为错误与启动信息 |
