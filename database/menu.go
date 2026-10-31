package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------- 菜单 ----------

// GetOrCreateMenu 取某周的菜单 ID，不存在则创建
func (d *Database) GetOrCreateMenu(weekStart string) (int64, error) {
	var id int64
	err := d.db.QueryRow("SELECT id FROM menus WHERE week_start = ?", weekStart).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	res, err := d.db.Exec("INSERT INTO menus (week_start, created_at) VALUES (?,?)",
		weekStart, time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetMenu 取某周完整菜单（含每道菜的详情）。没有菜单时返回 nil
func (d *Database) GetMenu(weekStart string) (*Menu, error) {
	var menu Menu
	err := d.db.QueryRow("SELECT id, week_start FROM menus WHERE week_start = ?", weekStart).
		Scan(&menu.ID, &menu.WeekStart)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := d.db.Query(`SELECT id, menu_id, day_index, slot, seq, dish_id, locked
        FROM menu_items WHERE menu_id = ?
        ORDER BY day_index, CASE slot WHEN 'breakfast' THEN 0 WHEN 'lunch' THEN 1 ELSE 2 END, seq`, menu.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	menu.Items = []MenuItem{}
	for rows.Next() {
		var item MenuItem
		var locked int
		if err := rows.Scan(&item.ID, &item.MenuID, &item.DayIndex, &item.Slot, &item.Seq, &item.DishID, &locked); err != nil {
			return nil, err
		}
		item.Locked = locked != 0
		menu.Items = append(menu.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 菜品单独全量取一次再挂上去，比 LEFT JOIN 逐行扫 JSON 列更省心；
	// 菜库规模在百来条，一次查询的开销可以忽略
	dishes, err := d.ListDishes(DishFilter{IncludeBlocked: true})
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*Dish, len(dishes))
	for i := range dishes {
		byID[dishes[i].ID] = &dishes[i]
	}
	for i := range menu.Items {
		if dish, ok := byID[menu.Items[i].DishID]; ok {
			menu.Items[i].Dish = dish
		}
	}

	return &menu, nil
}

// SaveMenuItems 用给定的格子整体覆盖某周菜单（生成与复用都走这里）
func (d *Database) SaveMenuItems(menuID int64, items []MenuItem) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM menu_items WHERE menu_id = ?", menuID); err != nil {
		return err
	}
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO menu_items (menu_id, day_index, slot, seq, dish_id, locked)
            VALUES (?,?,?,?,?,?)`, menuID, it.DayIndex, it.Slot, it.Seq, it.DishID, boolToInt(it.Locked)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetMenuItem 新增或更新某一格
func (d *Database) SetMenuItem(menuID int64, dayIndex int, slot string, seq int, dishID int64) error {
	_, err := d.db.Exec(`INSERT INTO menu_items (menu_id, day_index, slot, seq, dish_id, locked)
        VALUES (?,?,?,?,?,0)
        ON CONFLICT(menu_id, day_index, slot, seq) DO UPDATE SET dish_id=excluded.dish_id`,
		menuID, dayIndex, slot, seq, dishID)
	return err
}

// SetMenuItemLocked 切换某格的锁定状态
func (d *Database) SetMenuItemLocked(menuID int64, dayIndex int, slot string, seq int, locked bool) error {
	_, err := d.db.Exec(`UPDATE menu_items SET locked=? WHERE menu_id=? AND day_index=? AND slot=? AND seq=?`,
		boolToInt(locked), menuID, dayIndex, slot, seq)
	return err
}

// ListWeeks 列出已有菜单的周（倒序），用于历史回看与复用
func (d *Database) ListWeeks() ([]string, error) {
	rows, err := d.db.Query("SELECT week_start FROM menus ORDER BY week_start DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	weeks := []string{}
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		weeks = append(weeks, w)
	}
	return weeks, rows.Err()
}

// CopyMenu 把某周的菜单复制到目标周（目标周原有内容被覆盖）
func (d *Database) CopyMenu(fromWeek, toWeek string) error {
	src, err := d.GetMenu(fromWeek)
	if err != nil {
		return err
	}
	if src == nil || len(src.Items) == 0 {
		return fmt.Errorf("源周 %s 没有可复用的菜单", fromWeek)
	}

	targetID, err := d.GetOrCreateMenu(toWeek)
	if err != nil {
		return err
	}
	for i := range src.Items {
		src.Items[i].MenuID = targetID
	}
	return d.SaveMenuItems(targetID, src.Items)
}

// ---------- 采购清单 ----------

// ShoppingList 汇总某周所有菜的食材，按分类分组
// 用 SQLite 的 json_each 把 JSON 列的食材摊开，再在 Go 侧合并数量
func (d *Database) ShoppingList(menuID int64) ([]ShoppingGroup, error) {
	rows, err := d.db.Query(`SELECT je.value->>'cat', je.value->>'name', je.value->>'qty'
        FROM menu_items mi JOIN dishes d ON d.id = mi.dish_id, json_each(d.ingredients) je
        WHERE mi.menu_id = ?`, menuID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type agg struct {
		cat  string
		qtys []string
	}
	order := []string{}
	items := map[string]*agg{}

	for rows.Next() {
		var cat, name, qty sql.NullString
		if err := rows.Scan(&cat, &name, &qty); err != nil {
			return nil, err
		}
		key := cat.String + "|" + name.String
		if _, ok := items[key]; !ok {
			items[key] = &agg{cat: cat.String}
			order = append(order, key)
		}
		if q := strings.TrimSpace(qty.String); q != "" {
			items[key].qtys = append(items[key].qtys, q)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	groups := map[string][]ShoppingItem{}
	cats := []string{}
	for _, key := range order {
		a := items[key]
		name := key[strings.Index(key, "|")+1:]
		item := ShoppingItem{Name: name, Qty: mergeQty(a.qtys)}
		if _, ok := groups[a.cat]; !ok {
			cats = append(cats, a.cat)
		}
		groups[a.cat] = append(groups[a.cat], item)
	}
	sort.Strings(cats)

	out := make([]ShoppingGroup, 0, len(cats))
	for _, cat := range cats {
		out = append(out, ShoppingGroup{Cat: cat, Items: groups[cat]})
	}
	return out, nil
}

// mergeQty 合并同一食材的数量：单位一致且可解析时相加，否则并列显示
func mergeQty(qtys []string) string {
	if len(qtys) == 0 {
		return ""
	}

	seen := map[string]bool{}
	uniq := []string{}
	for _, q := range qtys {
		if !seen[q] {
			seen[q] = true
			uniq = append(uniq, q)
		}
	}
	if len(uniq) == 1 {
		return uniq[0]
	}

	// 尝试相加：要求数值部分都能解析、单位完全一致
	total := 0.0
	unit := ""
	for _, q := range qtys {
		num, u, ok := parseQty(q)
		if !ok {
			return strings.Join(uniq, " + ")
		}
		if unit == "" {
			unit = u
		} else if unit != u {
			return strings.Join(uniq, " + ")
		}
		total += num
	}
	return formatNum(total) + unit
}

// parseQty 把 "300g" 拆成 (300, "g")
func parseQty(s string) (float64, string, bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return 0, "", false
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, "", false
	}
	return num, strings.TrimSpace(s[i:]), true
}

func formatNum(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

// ---------- 采购清单勾选状态 ----------

// GetChecked 读取某周采购清单的勾选状态
func (d *Database) GetChecked(menuID int64) (map[string]bool, error) {
	var raw string
	if err := d.db.QueryRow("SELECT checked_json FROM menus WHERE id = ?", menuID).Scan(&raw); err != nil {
		return map[string]bool{}, nil
	}
	out := map[string]bool{}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]bool{}, nil
	}
	return out, nil
}

// SetChecked 保存某周采购清单的勾选状态
func (d *Database) SetChecked(menuID int64, checked map[string]bool) error {
	b, err := json.Marshal(checked)
	if err != nil {
		return err
	}
	_, err = d.db.Exec("UPDATE menus SET checked_json = ? WHERE id = ?", string(b), menuID)
	return err
}
