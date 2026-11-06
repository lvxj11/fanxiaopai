package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Database SQLite 封装
// 单进程写入，用 WAL + 单连接即可，不需要连接池
type Database struct {
	db *sql.DB
}

// InitDB 打开数据库并完成建表与结构迁移。
// 内置菜品的导入不再由启动触发，改由首次 /setup 时用户选择是否导入。
func InitDB(path string) (*Database, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %v", err)
	}
	db.SetMaxOpenConns(1)

	d := &Database{db: db}
	if err := d.createTables(); err != nil {
		db.Close()
		return nil, err
	}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

// Close 关闭数据库
func (d *Database) Close() error {
	return d.db.Close()
}

// createTables 建表。全部用 IF NOT EXISTS，重复启动安全
func (d *Database) createTables() error {
	schema := `
CREATE TABLE IF NOT EXISTS dishes (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    main_ingredient TEXT NOT NULL DEFAULT '素',
    kind            TEXT NOT NULL,
    slots           TEXT NOT NULL DEFAULT '[]',
    ingredients     TEXT NOT NULL DEFAULT '[]',
    tags            TEXT NOT NULL DEFAULT '[]',
    time_cost       INTEGER NOT NULL DEFAULT 20,
    can_pack        INTEGER NOT NULL DEFAULT 0,
    green           INTEGER NOT NULL DEFAULT 0,
    blocked         INTEGER NOT NULL DEFAULT 0,
    weight          REAL NOT NULL DEFAULT 1.0,
    source          TEXT NOT NULL DEFAULT 'seed',
    created_at      TEXT NOT NULL DEFAULT '',
    recipe          TEXT NOT NULL DEFAULT '',
    prep            TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS menus (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    week_start  TEXT NOT NULL UNIQUE,
    checked_json TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS menu_items (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    menu_id   INTEGER NOT NULL,
    day_index INTEGER NOT NULL,
    slot      TEXT NOT NULL,
    seq       INTEGER NOT NULL,
    dish_id   INTEGER NOT NULL,
    locked    INTEGER NOT NULL DEFAULT 0,
    UNIQUE(menu_id, day_index, slot, seq)
);

CREATE TABLE IF NOT EXISTS feedback (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    dish_id    INTEGER NOT NULL,
    menu_id    INTEGER NOT NULL DEFAULT 0,
    rated_date TEXT NOT NULL,
    rating     INTEGER NOT NULL,
    UNIQUE(dish_id, rated_date)
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_menu_items_menu ON menu_items(menu_id);
CREATE INDEX IF NOT EXISTS idx_feedback_dish ON feedback(dish_id);
`
	if _, err := d.db.Exec(schema); err != nil {
		return fmt.Errorf("建表失败: %v", err)
	}
	return nil
}

// ---------- 小工具 ----------

func unmarshalList(raw sql.NullString) []string {
	if !raw.Valid || raw.String == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return []string{}
	}
	return out
}

func marshalList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func unmarshalIngredients(raw sql.NullString) []Ingredient {
	if !raw.Valid || raw.String == "" {
		return []Ingredient{}
	}
	var out []Ingredient
	if err := json.Unmarshal([]byte(raw.String), &out); err != nil {
		return []Ingredient{}
	}
	return out
}

func marshalIngredients(v []Ingredient) string {
	if v == nil {
		v = []Ingredient{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------- 菜品 ----------

// dishColumns 含最近做过日期的查询列。
// day_index 是周内第几天（0=周一），配合 week_start 用 date() 算出实际日期
const dishColumns = `d.id, d.name, d.main_ingredient, d.kind, d.slots, d.ingredients, d.tags,
    d.time_cost, d.can_pack, d.green, d.blocked, d.weight, d.source, d.created_at, d.recipe, d.prep,
    (SELECT MAX(date(m.week_start, '+' || mi.day_index || ' days'))
       FROM menu_items mi JOIN menus m ON m.id = mi.menu_id
      WHERE mi.dish_id = d.id) AS last_made`

type dishScanner interface {
	Scan(dest ...any) error
}

func scanDish(row dishScanner) (*Dish, error) {
	var (
		d           Dish
		slots       sql.NullString
		ingredients sql.NullString
		tags        sql.NullString
		canPack     int
		green       int
		blocked     int
		source      sql.NullString
		createdAt   sql.NullString
		recipe      sql.NullString
		prep        sql.NullString
		lastMade    sql.NullString
	)
	if err := row.Scan(&d.ID, &d.Name, &d.MainIngredient, &d.Kind, &slots, &ingredients,
		&tags, &d.TimeCost, &canPack, &green, &blocked, &d.Weight, &source, &createdAt,
		&recipe, &prep, &lastMade); err != nil {
		return nil, err
	}
	d.Slots = unmarshalList(slots)
	d.Ingredients = unmarshalIngredients(ingredients)
	d.Tags = unmarshalList(tags)
	d.CanPack = canPack != 0
	d.Green = green != 0
	d.Blocked = blocked != 0
	d.Source = source.String
	d.CreatedAt = createdAt.String
	d.Recipe = recipe.String
	d.Prep = prep.String
	d.LastMade = lastMade.String
	return &d, nil
}

// ListDishes 列出菜品，可按类型/主料/关键词筛选
func (d *Database) ListDishes(f DishFilter) ([]Dish, error) {
	where := []string{}
	args := []any{}

	if f.Kind != "" {
		where = append(where, "d.kind = ?")
		args = append(args, f.Kind)
	}
	if f.MainIngredient != "" {
		where = append(where, "d.main_ingredient = ?")
		args = append(args, f.MainIngredient)
	}
	if f.Query != "" {
		where = append(where, "(d.name LIKE ? OR EXISTS (SELECT 1 FROM json_each(d.ingredients) je WHERE je.value->>'name' LIKE ?))")
		args = append(args, "%"+f.Query+"%", "%"+f.Query+"%")
	}
	if !f.IncludeBlocked {
		where = append(where, "d.blocked = 0")
	}

	query := "SELECT " + dishColumns + " FROM dishes d"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY d.kind, d.main_ingredient, d.name"

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dishes := []Dish{}
	for rows.Next() {
		dish, err := scanDish(rows)
		if err != nil {
			return nil, err
		}
		dishes = append(dishes, *dish)
	}
	return dishes, rows.Err()
}

// GetDish 按 ID 取菜品
func (d *Database) GetDish(id int64) (*Dish, error) {
	row := d.db.QueryRow("SELECT "+dishColumns+" FROM dishes d WHERE d.id = ?", id)
	dish, err := scanDish(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return dish, err
}

// CreateDish 新增菜品
func (d *Database) CreateDish(dish *Dish) error {
	if dish.Source == "" {
		dish.Source = "user"
	}
	if dish.CreatedAt == "" {
		dish.CreatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	if dish.Weight == 0 {
		dish.Weight = 1.0
	}
	res, err := d.db.Exec(`INSERT INTO dishes
        (name, main_ingredient, kind, slots, ingredients, tags, time_cost, can_pack, green, blocked, weight, source, created_at, recipe, prep)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		dish.Name, dish.MainIngredient, dish.Kind, marshalList(dish.Slots), marshalIngredients(dish.Ingredients),
		marshalList(dish.Tags), dish.TimeCost, boolToInt(dish.CanPack), boolToInt(dish.Green),
		boolToInt(dish.Blocked), dish.Weight, dish.Source, dish.CreatedAt, dish.Recipe, dish.Prep)
	if err != nil {
		return err
	}
	dish.ID, _ = res.LastInsertId()
	return nil
}

// UpdateDish 更新菜品（不含 weight/blocked，那两个由反馈维护）
func (d *Database) UpdateDish(dish *Dish) error {
	_, err := d.db.Exec(`UPDATE dishes SET name=?, main_ingredient=?, kind=?, slots=?, ingredients=?,
        tags=?, time_cost=?, can_pack=?, green=?, recipe=?, prep=? WHERE id=?`,
		dish.Name, dish.MainIngredient, dish.Kind, marshalList(dish.Slots), marshalIngredients(dish.Ingredients),
		marshalList(dish.Tags), dish.TimeCost, boolToInt(dish.CanPack), boolToInt(dish.Green),
		dish.Recipe, dish.Prep, dish.ID)
	return err
}

// DeleteDish 删除菜品，同时清掉引用它的菜单格子
func (d *Database) DeleteDish(id int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM menu_items WHERE dish_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM feedback WHERE dish_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM dishes WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetDishBlocked 拉黑/恢复
func (d *Database) SetDishBlocked(id int64, blocked bool) error {
	_, err := d.db.Exec("UPDATE dishes SET blocked = ? WHERE id = ?", boolToInt(blocked), id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- 反馈 ----------

// AddFeedback 记录一次评分并调整权重。
// rating=0（不吃）直接拉黑，后续生成一律排除
func (d *Database) AddFeedback(dishID int64, menuID int64, rating int, date string) error {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO feedback (dish_id, menu_id, rated_date, rating) VALUES (?,?,?,?)
        ON CONFLICT(dish_id, rated_date) DO UPDATE SET rating=excluded.rating, menu_id=excluded.menu_id`,
		dishID, menuID, date, rating); err != nil {
		return err
	}

	switch rating {
	case RatingDislike:
		if _, err := tx.Exec("UPDATE dishes SET blocked=1, weight=0.2 WHERE id=?", dishID); err != nil {
			return err
		}
	case RatingLike:
		if _, err := tx.Exec("UPDATE dishes SET blocked=0, weight=MIN(2.5, weight+0.3) WHERE id=?", dishID); err != nil {
			return err
		}
	default: // 一般
		if _, err := tx.Exec("UPDATE dishes SET blocked=0, weight=MAX(0.6, weight-0.1) WHERE id=?", dishID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ---------- 设置 ----------

// GetSetting 读取设置，不存在时返回默认值
func (d *Database) GetSetting(key, fallback string) string {
	var value string
	if err := d.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value); err != nil {
		return fallback
	}
	return value
}

// SetSetting 写入设置
func (d *Database) SetSetting(key, value string) error {
	_, err := d.db.Exec(`INSERT INTO settings (key, value) VALUES (?,?)
        ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
