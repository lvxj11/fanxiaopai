# 04 模块与函数

按包组织，逐项说明关键类型、函数职责与调用关系。排班算法单独见 [05 排班算法](05-generator.md)。

---

## 1. `main`（[main.go](../main.go)）

进程入口，只做编排，不含业务逻辑。

| 步骤 | 代码 | 说明 |
|---|---|---|
| 参数解析 | `flag.Bool("v", ...)` | `-v` 输出版本后直接返回 |
| 配置 | `utils.LoadConfig()` | 失败打印并退出 |
| 日志 | `utils.InitLoggerWithLevel(config.LogLevel)` | 失败打印并退出；`defer log.Close()` |
| 数据库 | `database.InitDB(config.DBPath)` | 建表 + 条件导入种子；失败记录错误并退出 |
| 服务 | `web.NewServer(config, db).Start(config.WebPort)` | 阻塞运行 |

启动日志会打印日志级别、数据库路径、Web 端口。

---

## 2. `utils` 包

### 2.1 配置（[utils/config.go](../utils/config.go)）

```go
type Config struct {
    LogLevel string // debug / info / warn / error
    WebPort  string
    DBPath   string
}
```

| 函数 | 说明 |
|---|---|
| `LoadConfig() (*Config, error)` | 默认值 → 确保 `./data` → 读 `data/config.ini`（不存在则生成）→ 环境变量逐项覆盖 |
| `loadConfigFile(config, path) error` | 逐行解析 ini，只识别 `[server]` 段下的 `log_level` / `web_port` / `db_path`；忽略空行与 `#`、`;` 注释 |
| `applyEnvOverrides(config)` | `LOG_LEVEL` / `WEB_PORT` / `DB_PATH` 非空即覆盖 |
| `GenerateDefaultConfig(config) error` | 写入带中文注释的默认 `config.ini`；写失败只影响便利性 |

注意：

- ini 解析是手写的极简实现，不支持多段嵌套、转义、行内注释。
- 读文件失败（非「不存在」）会让 `LoadConfig` 返回错误并终止启动。
- 生成默认配置时会重复 `MkdirAll("./data")`，属于幂等操作。

### 2.2 日志（[utils/logger.go](../utils/logger.go)）

```go
type Logger struct {
    mu            sync.Mutex
    file          *os.File
    logPath       string
    maxSize       int64
    logLevel      string
    consoleOutput bool
}
```

| 成员 | 说明 |
|---|---|
| `NewLogger(path, maxSize, level, console) (*Logger, error)` | 建目录、追加打开文件；非法级别回退到 `warn` |
| `Close()` | 加锁关闭文件 |
| `shouldLog(level)` | 按 `levelOrder` 比较；`startup` 级别无条件输出 |
| `log(prefix, format, ...)` | 统一写入：加锁 → 尝试轮转 → 写时间戳行 → `Sync` → 可选控制台 |
| `rotateLocked()` | 超过 `maxSize` 时关闭文件、重命名为 `.old`、重建文件（调用方须持锁） |
| `SetConsoleOutput(bool)` | 服务启动后关闭控制台输出，避免请求日志刷屏 |
| `Debug/Info/Warn/Error/Startup(format, ...)` | 分级入口 |
| `InitLoggerWithLevel(level) error` | 初始化全局 `DefaultLogger`，固定路径 `./data/fanxiaopai.log`，10MB 上限，开启控制台 |
| `GetLogger() *Logger` | 未初始化时兜底创建（`warn` 级别）；失败直接 panic |

日志格式：`[2006-01-02 15:04:05] [LEVEL] 消息`。

### 2.3 版本（[utils/version.go](../utils/version.go)）

```go
const (
    Name     = "饭小排"
    FullName = "家常饭排班表"
    Version  = "0.3.3"
)
```

`-v` 输出与页面页脚均引用 `utils.Version`。

---

## 3. `database` 包

### 3.1 模型（[database/model.go](../database/model.go)）

| 类型 | 用途 |
|---|---|
| `Ingredient{Name, Cat, Qty}` | 菜品的一种食材；`Cat` 决定采购分组 |
| `Dish` | 菜品完整实体（含 `LastMade` 运行态字段，非表字段） |
| `MenuItem` | 菜单一格；`Dish *Dish` 为查询时挂载的详情，非表字段 |
| `Menu{ID, WeekStart, Items}` | 一周菜单 |
| `ShoppingItem{Name, Qty}` / `ShoppingGroup{Cat, Items}` | 采购清单行与分组 |
| `DishFilter{Kind, MainIngredient, Query, IncludeBlocked}` | 菜库列表筛选条件 |

### 3.2 连接、建表与菜品（[database/database.go](../database/database.go)）

| 函数 | 说明 |
|---|---|
| `InitDB(path) (*Database, error)` | `sql.Open("sqlite", path)`，`SetMaxOpenConns(1)`，然后建表、导入种子 |
| `(*Database) Close() error` | 关闭数据库 |
| `createTables() error` | 建立 5 张表与 2 个索引，全部 `IF NOT EXISTS` |
| `unmarshalList` / `marshalList` | `[]string` 与 JSON 文本互转，坏数据回退空切片 |
| `unmarshalIngredients` / `marshalIngredients` | `[]Ingredient` 与 JSON 文本互转 |
| `dishColumns`（常量） | 统一的菜品查询列清单，含 `last_made` 子查询 |
| `scanDish(row) (*Dish, error)` | 把一行扫进 `Dish`，处理 JSON 列与 0/1 布尔列 |
| `ListDishes(f DishFilter) ([]Dish, error)` | 支持按类型/主料/关键词筛选；关键词同时匹配菜名与 `json_each(ingredients)` 的食材名；默认排除拉黑 |
| `GetDish(id) (*Dish, error)` | 不存在返回 `(nil, nil)` |
| `CreateDish(dish) error` | 补默认 `source=user`、`created_at`、`weight=1.0` 后插入 |
| `UpdateDish(dish) error` | 只更新用户可改字段；**不含 `weight` / `blocked` / `source` / `created_at`** |
| `DeleteDish(id) error` | 事务：删菜单格 → 删反馈 → 删菜品 |
| `SetDishBlocked(id, bool) error` | 拉黑/恢复 |
| `boolToInt(bool) int` | 布尔转 0/1 |
| `AddFeedback(dishID, menuID, rating, date) error` | 事务：UPSERT 反馈 + 按评分调整 `blocked`/`weight` |
| `GetSetting(key, fallback) string` | 读设置，缺失或出错返回 fallback |
| `SetSetting(key, value) error` | UPSERT 设置 |

反馈规则（`AddFeedback`）：

| rating | blocked | weight |
|---|---|---|
| 0 不吃 | 1 | 固定 0.2 |
| 1 一般 | 0 | `MAX(0.6, weight - 0.1)` |
| 2 爱吃 | 0 | `MIN(2.5, weight + 0.3)` |

### 3.3 菜单与采购（[database/menu.go](../database/menu.go)）

| 函数 | 说明 |
|---|---|
| `GetOrCreateMenu(weekStart) (int64, error)` | 按周取 ID，不存在则插入 |
| `GetMenu(weekStart) (*Menu, error)` | 取菜单与全部格子，按天 → 餐次 → seq 排序；再全量取菜品并按 ID 挂到 `MenuItem.Dish`；无菜单返回 `(nil, nil)` |
| `SaveMenuItems(menuID, items) error` | 事务内先删后插，整周覆盖 |
| `SetMenuItem(menuID, day, slot, seq, dishID) error` | UPSERT 单格，不改变 `locked` |
| `SetMenuItemLocked(menuID, day, slot, seq, locked) error` | 切换锁定 |
| `SetAllMenuItemsLocked(menuID, locked) error` | 一次锁定/解锁整周所有格子（`UPDATE menu_items SET locked=? WHERE menu_id=?`） |
| `ListWeeks() ([]string, error)` | 已有菜单的周，倒序 |
| `CopyMenu(fromWeek, toWeek) error` | 复制源周格子到目标周（目标内容被覆盖）；源周为空时报错 |
| `ShoppingList(menuID) ([]ShoppingGroup, error)` | `json_each` 摊平食材，Go 侧按 `分类+名称` 聚合，分类排序输出 |
| `mergeQty([]string) string` | 去重后：单位与数值都可解析则求和，否则 `a + b` 并列 |
| `parseQty(s) (float64, string, bool)` | 拆分 `"300g"` → `(300, "g")`；无前导数字视为不可解析 |
| `formatNum(f) string` | 整数去小数，否则保留 1 位 |
| `GetChecked(menuID) (map[string]bool, error)` | 读 `checked_json`；任何异常都返回空 map |
| `SetChecked(menuID, map) error` | 覆盖写勾选 JSON |

`GetMenu` 的排序 SQL：

```sql
ORDER BY day_index,
         CASE slot WHEN 'breakfast' THEN 0 WHEN 'lunch' THEN 1 ELSE 2 END,
         seq
```

### 3.4 种子（[database/seed.go](../database/seed.go)）

| 类型/函数 | 说明 |
|---|---|
| `seedFS` | `//go:embed seed/dishes.json` 的嵌入文件系统 |
| `seedFile` / `seedDish` | 种子文件结构；`seedDish` 不含 ID/权重等运行态字段 |
| `validMainIngredients` / `validKinds` / `validSlots` | 种子校验枚举 |
| `loadSeed() error` | 仅当菜库为空时全量导入，单事务 |
| `RestoreSeedDishes() (int, error)` | 只补缺失菜名，返回补入数量 |
| `parseSeed() (*seedFile, error)` | 读取 + 逐条校验，错误信息带序号和菜名 |

---

## 4. `generator` 包

对外只暴露少量类型，细节见 [05 排班算法](05-generator.md)。

```go
var ErrNoDishes = errors.New("菜品库里没有可用的菜，先去菜库添加几道吧")

type Cell struct { DayIndex int; Slot string; Seq int }

type Request struct {
    WeekStart string
    MealSlots map[int][]string
    Dishes    []database.Dish
    AvoidTags []string
    Existing  map[Cell]int64
    Locked    map[Cell]bool
    Target    *Cell
    Now       time.Time
}

type Item struct { Cell; DishID int64 }

type Result struct {
    Items    []Item
    Relaxed  int
    Warnings []string
}

type Generator interface {
    Generate(ctx context.Context, req Request) (*Result, error)
}
```

`LocalWeighted` 是当前唯一实现，`NewLocalWeighted(rng *rand.Rand)` 允许注入固定随机源以便测试。

---

## 5. `ai` 包（[ai/provider.go](../ai/provider.go)）

```go
var ErrNotConfigured = errors.New("未配置 AI 服务")

type Provider interface {
    Configured() bool
    Name() string
    Suggest(ctx context.Context, req SuggestRequest) ([]SuggestedDish, error)
}
```

| 类型 | 说明 |
|---|---|
| `SuggestRequest` | 餐次、类型、忌口、已有菜名、期望数量 |
| `SuggestedDish` | 与菜库字段对齐的推荐菜（含 `note` 说明） |
| `Item` | 推荐菜的食材 |
| `NoopProvider` | 占位实现：`Configured=false`、`Name="未配置"`、`Suggest` 返回 `ErrNotConfigured` |

接入真实模型时新增一个 `Provider` 实现并在 `web.NewServer` 替换即可，调用方与页面无需改动（`web.Server.ai` 字段）。

---

## 6. `web` 包

### 6.1 服务器与通用工具（[web/web.go](../web/web.go)）

```go
type Server struct {
    config    *utils.Config
    db        *database.Database
    generator generator.Generator
    ai        ai.Provider
}
```

| 函数 | 说明 |
|---|---|
| `NewServer(config, db) *Server` | 装配 `LocalWeighted` 与 `NoopProvider` |
| `Handler() http.Handler` | 注册全部路由，返回包了 gzip 的 handler |
| `Start(port string)` | 打印启动日志、关闭控制台输出、`ListenAndServe` |
| `redirectToMenu` | `/` 302 到 `/menu`；其他未知路径 404 |
| `showMenu/showDishes/showShopping/showSettings` | 调用 `renderPage` 渲染对应页面 |
| `renderPage(w, title, currentPage, contentTemplate)` | 解析 `base.html` + 内容模板，注入 `Title/CurrentPage/Version/Today` |
| `serveStatic() http.Handler` | 嵌入静态文件服务；`sw.js` 加 `Service-Worker-Allowed: /` 与 no-cache；`.webmanifest` 修正 MIME |
| `serveFavicon` | 输出 `favicon.svg`，缓存 1 天 |
| `weekParam(r)` | 读取 `?week=`，非法或缺失时取本周一 |
| `mondayOf(t)` | 计算所在周周一（`day_index=0` 对应周一） |
| `isValidDate(s)` | 严格 `YYYY-MM-DD` 校验 |
| `writeJSON` / `writeError` | 统一 JSON 输出；错误体为 `{"error": "..."}` |
| `readJSON(w, r, dst)` | 请求体上限 1MB 并解析 JSON |
| `(*Server) apiError(w, err, fallback)` | `ErrNoDishes` / `ErrNotConfigured` 走 400 并用原文；其他记日志后返回 500 + fallback 文案 |
| `requirePOST(w, r) bool` | 非 POST 返回 405 |
| `apiHealth` | `{"status":"ok","version":...}` |
| `apiAISuggest` | 解析请求，调用 `s.ai.Suggest`；未接入时返回 501 |
| `gzipMiddleware` | 客户端接受 gzip 时压缩响应，设置 `Content-Encoding`、`Vary`，删除 `Content-Length` |

### 6.2 菜单与采购 API（[web/api_menu.go](../web/api_menu.go)）

| 函数 | 说明 |
|---|---|
| `apiMenu` | GET 某周菜单 + 历史周 + 排餐配置 |
| `apiGenerateMenu` | 生成/重排整周；`keep_locked=true` 时把已有菜单交给生成器 |
| `apiCopyMenu` | 复制历史周菜单到目标周 |
| `apiRerollItem` | 单格随机换菜，只写回目标格 |
| `apiReplaceItem` | 指定菜替换单格（PUT） |
| `apiLockItem` | 切换单格锁定 |
| `apiLockAll` | 一次锁定/解锁整周（POST `/api/menu/lock-all`，body `{week, locked}`），配合前端「全部锁定/解锁」与合并后的生成按钮 |
| `apiShopping` | 采购清单 + 勾选状态 |
| `apiShoppingChecked` | 保存勾选状态（PUT） |
| `writeMenuResult` | 重新读菜单与历史周，返回 `menu/weeks/relaxed/warnings` |
| `loadMenu(w, week)` | 校验周格式并读取菜单，空菜单返回 404 |
| `splitExisting(menu)` | 拆成 `map[generator.Cell]int64` 与 `map[generator.Cell]bool` |
| `mealSlots()` | 读 `meal_slots` 设置，解析失败回退默认，并补齐 7 天键 |
| `mealSlotsByDay()` | 字符串键转整型键，供生成器使用 |
| `avoidTags()` | 读 `avoid_tags`，解析失败按无忌口处理 |

### 6.3 菜品与反馈 API（[web/api_dishes.go](../web/api_dishes.go)）

| 函数 | 说明 |
|---|---|
| `apiDishes` | GET 列表 / POST 新增，其余 405 |
| `listDishes` | 支持 `kind` / `main` / `q` / `blocked=1` 筛选，并汇总全库标签供忌口选项 |
| `createDish` | 校验后新增；UNIQUE 冲突转中文提示 |
| `apiDish` | 解析 `/api/dishes/{id}[/blocked|/feedback]` 与 `/api/dishes/restore-seed` |
| `updateDish` / `deleteDish` | 编辑 / 删除 |
| `setBlocked` | 拉黑/恢复（POST） |
| `addFeedback` | 评分写入，校验只能是 0/1/2 |
| `restoreSeed` | 补回内置菜（POST），返回补入数量 |
| `validateDish(d) string` | 字段校验：菜名、主料、类型、餐次、耗时、至少一个食材且名称非空 |

`validMainIngredients` / `validKinds` / `validSlots` 在 web 层也维护了一份，用于输入校验（与 `database/seed.go` 中的种子校验相互独立）。

### 6.4 设置 API（[web/api_settings.go](../web/api_settings.go)）

| 函数 | 说明 |
|---|---|
| `apiMealSlots` | GET 返回当前排餐；PUT 清洗（只保留合法餐次、补齐 7 天）后保存 |
| `apiAvoidTags` | GET 返回忌口；PUT 保存，nil 归一为 `[]` |

### 6.5 离线快照（[web/export.go](../web/export.go)）

| 类型/函数 | 说明 |
|---|---|
| `exportData` | 模板根数据：标题、周范围、天数、采购分组、总数、版本、内联 CSS |
| `exportDay` / `exportSlot` / `exportGroup` / `exportCheck` | 快照视图结构 |
| `exportSnapshot` | 读取 CSS 与菜单/采购数据，渲染自包含 HTML；`download=1` 时作为附件 |
| `exportDays(week, menu, slotsByDay)` | 按配置的餐次组装 7 天，日期由周一推算，餐内按 `seq` 排序 |
| `exportGroups(groups, checked)` | 补分类 emoji 与勾选状态（键为 `cat|name`） |

快照模板 [web/templates/export.html](../web/templates/export.html) 不含任何 JS，勾选划掉效果由 CSS `:checked ~ .snap-name` 实现。
