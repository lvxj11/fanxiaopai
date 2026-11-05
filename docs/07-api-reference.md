# 07 API 参考

所有接口均为同源 HTTP JSON API（`/export` 除外，返回 HTML）。请求体大小上限 1MB，Content-Type 约定为 `application/json`。

## 1. 通用约定

### 1.1 周参数

- 周以**周一日期** `YYYY-MM-DD` 标识，`day_index` 0=周一 … 6=周日。
- GET 接口用 `?week=`；未传或格式非法时默默取本周一。
- 写接口在请求体里传 `week`；格式非法时：
  - `generate`：回退到本周一；
  - 其他写接口 / `copy`：返回 400。

### 1.2 成功与错误格式

成功响应为对象（除 `/export`）。错误统一为：

```json
{ "error": "中文提示" }
```

常见状态码：

| 状态码 | 场景 |
|---|---|
| 200 | 成功 |
| 400 | 请求格式有误、字段校验失败、`ErrNoDishes`、`ErrNotConfigured` |
| 404 | 菜单不存在、菜品 ID 缺失、未知子路径 |
| 405 | 方法不支持 |
| 500 | 数据库等内部错误（响应文案为固定中文提示） |
| 501 | AI 未接入（`/api/ai/suggest`） |

### 1.3 典型数据对象

**Dish**

```json
{
  "id": 12,
  "name": "番茄炒蛋",
  "main_ingredient": "蛋",
  "kind": "快手",
  "slots": ["breakfast", "lunch", "dinner"],
  "ingredients": [
    { "name": "鸡蛋", "cat": "肉蛋", "qty": "3个" },
    { "name": "番茄", "cat": "蔬菜", "qty": "2个" }
  ],
  "tags": [],
  "time_cost": 15,
  "can_pack": true,
  "green": false,
  "blocked": false,
  "weight": 1,
  "source": "seed",
  "created_at": "",
  "last_made": "2026-11-02",
  "recipe": "……",
  "prep": "……"
}
```

`last_made` 为查询时计算的最近一次被排入菜单的日期，无记录时为空串或省略。

**MenuItem**

```json
{
  "id": 31,
  "menu_id": 5,
  "day_index": 0,
  "slot": "dinner",
  "seq": 0,
  "dish_id": 12,
  "locked": false,
  "dish": { "...": "Dish 对象" }
}
```

**Menu**

```json
{ "id": 5, "week_start": "2026-11-02", "items": [ "MenuItem..." ] }
```

---

## 2. 菜单

### 2.1 获取某周菜单

```http
GET /api/menu?week=2026-11-02
```

响应：

```json
{
  "week": "2026-11-02",
  "menu": { "id": 5, "week_start": "2026-11-02", "items": [] },
  "weeks": ["2026-11-02", "2026-10-26"],
  "meal_slots": {
    "0": ["breakfast", "dinner"],
    "5": ["breakfast", "lunch", "dinner"]
  }
}
```

- 该周没有菜单时 `menu` 为 `null`。
- `weeks` 为已有菜单的周，倒序。
- `meal_slots` 始终补齐 7 天键，值可能为空数组。

未限制请求方法（非 GET 也会返回相同内容）。

### 2.2 生成 / 重排整周菜单

```http
POST /api/menu/generate
Content-Type: application/json

{ "week": "2026-11-02", "keep_locked": true }
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `week` | 否 | 非法则默认本周一 |
| `keep_locked` | 否 | true 时把当前菜单交给生成器（页面文案为「只重排没锁定的格子」） |

响应（`writeMenuResult`）：

```json
{
  "week": "2026-11-02",
  "menu": { "...": "最新菜单" },
  "weeks": ["2026-11-02"],
  "relaxed": 0,
  "warnings": null
}
```

- `relaxed`：放宽级别 0–4，0 表示完全满足约束。
- `warnings`：放宽说明数组，例如 `["绿叶菜不再保证每天都有"]`；无放宽时为 `null`。
- 菜库为空 → 400，`error` 为生成器的中文提示。
- `keep_locked=true` 会把当前菜单交给生成器（`splitExisting` 拆成 `Existing`/`Locked`）；`Generate` 内部先收敛 `Existing`，只重排未锁定格，不会产出重复格子（v0.3.2 已修复，见 [05 排班算法 §8](05-generator.md) 与回归测试 `TestKeepLockedRerollHasNoDuplicateCells`）。

### 2.3 复用历史菜单

```http
POST /api/menu/copy

{ "from_week": "2026-10-26", "to_week": "2026-11-02" }
```

- 两个字段都必须是合法日期，否则 400。
- 源周没有菜单或为空 → 500「复用菜单失败」，日志记录具体原因。
- 目标周原内容被覆盖。
- 响应同 `writeMenuResult`，`relaxed=0`、`warnings=null`。

### 2.4 单格随机换菜

```http
POST /api/menu/item/reroll

{ "week": "2026-11-02", "day_index": 2, "slot": "dinner", "seq": 0 }
```

- 周不存在/为空 → 404。
- 只重排目标格，其他格子保持不变。
- 响应同 `writeMenuResult`。
- 失败：菜库无可用菜 → 400。

### 2.5 指定菜替换单格

```http
PUT /api/menu/item/replace

{ "week": "2026-11-02", "day_index": 2, "slot": "dinner", "seq": 0, "dish_id": 12 }
```

- 只接受 PUT，否则 405。
- 直接 UPSERT 该格，不校验菜是否适合该餐次/类型（这是「用户强制指定」的语义）。
- 响应同 `writeMenuResult`。

### 2.6 锁定 / 解锁单格

```http
POST /api/menu/item/lock

{ "week": "2026-11-02", "day_index": 2, "slot": "dinner", "seq": 0, "locked": true }
```

- 只更新 `locked`，不改变菜。
- 格子不存在时 SQL 更新 0 行，仍返回成功。
- 响应同 `writeMenuResult`。

### 2.7 全部锁定 / 解锁整周

```http
POST /api/menu/lock-all

{ "week": "2026-11-02", "locked": true }
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `week` | 否 | 该周须已有菜单，否则 404 |
| `locked` | 否 | `true` 全锁、`false` 全解锁 |

- `UPDATE menu_items SET locked=? WHERE menu_id=?`，一次更新整周所有格子。
- 典型流程：先 `locked:true` 全部锁定 → 对不满意的几道用 §2.6 单独解锁 → `POST /api/menu/generate {keep_locked:true}` 只重排那几道。
- 响应同 `writeMenuResult`。

---

## 3. 采购清单

### 3.1 获取采购清单

```http
GET /api/shopping?week=2026-11-02
```

响应：

```json
{
  "week": "2026-11-02",
  "groups": [
    {
      "cat": "肉蛋",
      "items": [
        { "name": "鸡蛋", "qty": "6个" },
        { "name": "鸡胸肉", "qty": "300g" }
      ]
    }
  ],
  "checked": { "肉蛋|鸡蛋": true }
}
```

- `groups` 按分类名排序（`sort.Strings`）。
- `qty` 合并规则：单位一致且数值可解析时相加，否则用 `" + "` 并列。
- 该周无菜单 → 404。
- `checked` 键格式为 `"分类|食材名"`。

### 3.2 保存勾选状态

```http
PUT /api/shopping/checked

{ "week": "2026-11-02", "checked": { "肉蛋|鸡蛋": true } }
```

响应：`{"ok": true}`。

---

## 4. 菜品库

### 4.1 列表

```http
GET /api/dishes?kind=荤菜&main=鸡&q=鸡丁&blocked=1
```

| 参数 | 说明 |
|---|---|
| `kind` | 精确匹配类型 |
| `main` | 精确匹配主料 |
| `q` | 模糊匹配菜名或食材名 |
| `blocked=1` | 包含被拉黑的菜（默认排除） |

响应：

```json
{
  "dishes": [ "Dish..." ],
  "tags": ["辛辣", "花生", "海鲜"]
}
```

`tags` 来自**全部菜品**（含拉黑）去重后的标签集合，供设置页忌口选项使用。忽略 `kind/main/q` 筛选。

### 4.2 新增

```http
POST /api/dishes

{ "name": "土豆丝", "main_ingredient": "素", "kind": "素菜", "slots": ["lunch","dinner"],
  "ingredients": [{ "name": "土豆", "cat": "蔬菜", "qty": "2个" }], "time_cost": 15 }
```

校验规则（`validateDish`）：

- 菜名非空；
- `main_ingredient` ∈ 鸡/猪/牛/鱼/虾/蛋/豆腐/素；
- `kind` ∈ 荤菜/素菜/汤/主食/快手；
- `slots` 至少一个且合法；
- `time_cost > 0`；
- `ingredients` 至少一个，且每项 `name` 非空。

响应：`{"dish": Dish}`。重名 → 400「已经有同名的一道菜了」。

### 4.3 编辑

```http
PUT /api/dishes/12
{ "name": "...", "main_ingredient": "...", "kind": "...", "slots": [...], "ingredients": [...], "time_cost": 20, "can_pack": true, "green": false, "recipe": "...", "prep": "..." }
```

- 不修改 `weight` / `blocked` / `source` / `created_at`。
- 响应：`{"dish": Dish}`。

### 4.4 删除

```http
DELETE /api/dishes/12
```

同时删除引用该菜的菜单格与反馈记录。响应：`{"ok": true}`。

### 4.5 拉黑 / 恢复

```http
POST /api/dishes/12/blocked

{ "blocked": true }
```

响应：`{"ok": true}`。

### 4.6 反馈打分

```http
POST /api/dishes/12/feedback

{ "rating": 2, "date": "2026-11-05", "menu_id": 5 }
```

| 字段 | 说明 |
|---|---|
| `rating` | 必须是 0/1/2，否则 400「评分只能是 0/1/2」 |
| `date` | 可选，缺省当天；同一天同菜重复评分覆盖旧值 |
| `menu_id` | 可选，仅记录来源 |

权重联动：0 → 拉黑且权重 0.2；1 → 权重 `max(0.6, w-0.1)`；2 → 权重 `min(2.5, w+0.3)`。

响应：`{"ok": true}`。

### 4.7 补回内置菜品

```http
POST /api/dishes/restore-seed
```

只补库中不存在的内置菜，不覆盖已有记录。响应：`{"added": 3}`。

---

## 5. 设置

### 5.1 排餐配置

```http
GET /api/settings/meal-slots
```

```json
{ "meal_slots": { "0": ["breakfast","dinner"], "...": [] } }
```

```http
PUT /api/settings/meal-slots

{ "meal_slots": { "0": ["breakfast","dinner"], "5": ["breakfast","lunch","dinner"] } }
```

- 后端只保留合法餐次，并强制补齐 7 天键。
- 空数组表示当天不排任何餐。
- 响应为清洗后的完整配置。

### 5.2 忌口标签

```http
GET /api/settings/avoid-tags
```

```json
{ "avoid_tags": ["花生", "海鲜"] }
```

```http
PUT /api/settings/avoid-tags

{ "avoid_tags": ["花生"] }
```

- `null` 归一为 `[]`。
- 设置后需重新生成菜单才生效。

---

## 6. 其他

### 6.1 AI 换新鲜（预留）

```http
POST /api/ai/suggest

{ "slot": "dinner", "kind": "荤菜", "avoid_tags": ["花生"], "known_dishes": ["宫保鸡丁"], "count": 3 }
```

- `count <= 0` 时默认 3。
- 当前 `NoopProvider` 固定返回 501：

```json
{ "error": "AI 换新鲜暂不可用：未配置 AI 服务" }
```

- 接入真实 `Provider` 后响应为 `{"dishes": [SuggestedDish...]}`。

### 6.2 健康检查

```http
GET /api/health
```

```json
{ "status": "ok", "version": "0.3.3" }
```

### 6.3 离线快照

```http
GET /export?week=2026-11-02
GET /export?week=2026-11-02&download=1
```

- 返回 `text/html; charset=utf-8`，内容为自包含页面。
- `download=1` 时附加：

```http
Content-Disposition: attachment; filename="fanxiaopai-2026-11-02.html"; filename*=UTF-8''%E9%A5%8F%E6%8B%89...
```

- 错误（读 CSS/菜单/采购失败）以 `http.Error` 返回纯文本。

---

## 7. curl 示例

```bash
# 1. 查看本周菜单
curl -s 'http://localhost:8080/api/menu' | jq

# 2. 生成菜单（覆盖已有内容）
curl -s -X POST http://localhost:8080/api/menu/generate \
  -H 'Content-Type: application/json' \
  -d '{"week":"2026-11-02"}' | jq '.menu.items | length, .relaxed'

# 3. 单格换菜
curl -s -X POST http://localhost:8080/api/menu/item/reroll \
  -H 'Content-Type: application/json' \
  -d '{"week":"2026-11-02","day_index":2,"slot":"dinner","seq":0}' | jq

# 4. 打分（爱吃）
curl -s -X POST http://localhost:8080/api/dishes/12/feedback \
  -H 'Content-Type: application/json' -d '{"rating":2}'

# 5. 采购清单
curl -s 'http://localhost:8080/api/shopping?week=2026-11-02' | jq

# 6. 离线快照下载
curl -sL 'http://localhost:8080/export?week=2026-11-02&download=1' -o snapshot.html
```
