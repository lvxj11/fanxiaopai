package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"fanxiaopai/database"
	"fanxiaopai/generator"
	"fanxiaopai/utils"
)

// menuPayload 菜单页要用的全部数据，一次取齐省得前端来回请求
type menuPayload struct {
	Week      string              `json:"week"`
	Menu      *database.Menu      `json:"menu"`
	Weeks     []string            `json:"weeks"`
	MealSlots map[string][]string `json:"meal_slots"`
}

// apiMenu 取某周菜单
func (s *Server) apiMenu(w http.ResponseWriter, r *http.Request) {
	week := weekParam(r)

	menu, err := s.db.GetMenu(week)
	if err != nil {
		s.apiError(w, err, "读取菜单失败")
		return
	}

	weeks, err := s.db.ListWeeks()
	if err != nil {
		s.apiError(w, err, "读取历史菜单失败")
		return
	}

	writeJSON(w, http.StatusOK, menuPayload{
		Week:      week,
		Menu:      menu,
		Weeks:     weeks,
		MealSlots: s.mealSlots(),
	})
}

// apiGenerateMenu 生成或重新生成某周菜单
func (s *Server) apiGenerateMenu(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	var body struct {
		Week       string `json:"week"`
		KeepLocked bool   `json:"keep_locked"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	week := body.Week
	if !isValidDate(week) {
		week = mondayOf(time.Now())
	}

	menuID, err := s.db.GetOrCreateMenu(week)
	if err != nil {
		s.apiError(w, err, "创建菜单失败")
		return
	}

	// 保留锁定时，把已排好的格子原样交给生成器，它只填剩下的空格
	existing := map[generator.Cell]int64{}
	locked := map[generator.Cell]bool{}
	if body.KeepLocked {
		if menu, err := s.db.GetMenu(week); err == nil && menu != nil {
			existing, locked = splitExisting(menu)
		}
	}

	dishes, err := s.db.ListDishes(database.DishFilter{IncludeBlocked: true})
	if err != nil {
		s.apiError(w, err, "读取菜品库失败")
		return
	}

	result, err := s.generator.Generate(r.Context(), generator.Request{
		WeekStart: week,
		MealSlots: s.mealSlotsByDay(),
		Dishes:    dishes,
		AvoidTags: s.avoidTags(),
		Existing:  existing,
		Locked:    locked,
	})
	if err != nil {
		s.apiError(w, err, "生成菜单失败")
		return
	}

	items := make([]database.MenuItem, 0, len(result.Items))
	for _, it := range result.Items {
		items = append(items, database.MenuItem{
			MenuID:   menuID,
			DayIndex: it.DayIndex,
			Slot:     it.Slot,
			Seq:      it.Seq,
			DishID:   it.DishID,
			Locked:   locked[it.Cell],
		})
	}
	if err := s.db.SaveMenuItems(menuID, items); err != nil {
		s.apiError(w, err, "保存菜单失败")
		return
	}

	s.writeMenuResult(w, week, result.Relaxed, result.Warnings)
}

// apiCopyMenu 复用某一周的菜单
func (s *Server) apiCopyMenu(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	var body struct {
		FromWeek string `json:"from_week"`
		ToWeek   string `json:"to_week"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	if !isValidDate(body.FromWeek) || !isValidDate(body.ToWeek) {
		writeError(w, http.StatusBadRequest, "周日期格式应为 YYYY-MM-DD")
		return
	}

	if err := s.db.CopyMenu(body.FromWeek, body.ToWeek); err != nil {
		s.apiError(w, err, "复用菜单失败")
		return
	}
	s.writeMenuResult(w, body.ToWeek, 0, nil)
}

// apiRerollItem 只换某一格的菜，其余格子保持不动
func (s *Server) apiRerollItem(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	var body struct {
		Week     string `json:"week"`
		DayIndex int    `json:"day_index"`
		Slot     string `json:"slot"`
		Seq      int    `json:"seq"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}

	menuID, menu, ok := s.loadMenu(w, body.Week)
	if !ok {
		return
	}
	existing, _ := splitExisting(menu)
	target := generator.Cell{DayIndex: body.DayIndex, Slot: body.Slot, Seq: body.Seq}

	dishes, err := s.db.ListDishes(database.DishFilter{IncludeBlocked: true})
	if err != nil {
		s.apiError(w, err, "读取菜品库失败")
		return
	}

	result, err := s.generator.Generate(r.Context(), generator.Request{
		WeekStart: body.Week,
		MealSlots: s.mealSlotsByDay(),
		Dishes:    dishes,
		AvoidTags: s.avoidTags(),
		Existing:  existing,
		Target:    &target,
	})
	if err != nil {
		s.apiError(w, err, "换菜失败")
		return
	}

	for _, it := range result.Items {
		if it.Cell == target {
			if err := s.db.SetMenuItem(menuID, it.DayIndex, it.Slot, it.Seq, it.DishID); err != nil {
				s.apiError(w, err, "保存菜单失败")
				return
			}
		}
	}
	s.writeMenuResult(w, body.Week, result.Relaxed, result.Warnings)
}

// apiReplaceItem 把某一格换成用户指定的菜
func (s *Server) apiReplaceItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "只支持 PUT")
		return
	}

	var body struct {
		Week     string `json:"week"`
		DayIndex int    `json:"day_index"`
		Slot     string `json:"slot"`
		Seq      int    `json:"seq"`
		DishID   int64  `json:"dish_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}

	menuID, _, ok := s.loadMenu(w, body.Week)
	if !ok {
		return
	}
	if err := s.db.SetMenuItem(menuID, body.DayIndex, body.Slot, body.Seq, body.DishID); err != nil {
		s.apiError(w, err, "保存菜单失败")
		return
	}
	s.writeMenuResult(w, body.Week, 0, nil)
}

// apiLockItem 切换某一格的锁定状态
func (s *Server) apiLockItem(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	var body struct {
		Week     string `json:"week"`
		DayIndex int    `json:"day_index"`
		Slot     string `json:"slot"`
		Seq      int    `json:"seq"`
		Locked   bool   `json:"locked"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}

	menuID, _, ok := s.loadMenu(w, body.Week)
	if !ok {
		return
	}
	if err := s.db.SetMenuItemLocked(menuID, body.DayIndex, body.Slot, body.Seq, body.Locked); err != nil {
		s.apiError(w, err, "保存菜单失败")
		return
	}
	s.writeMenuResult(w, body.Week, 0, nil)
}

// ---------- 采购清单 ----------

// apiShopping 取某周的采购清单
func (s *Server) apiShopping(w http.ResponseWriter, r *http.Request) {
	week := weekParam(r)
	_, menu, ok := s.loadMenu(w, week)
	if !ok {
		return
	}

	groups, err := s.db.ShoppingList(menu.ID)
	if err != nil {
		s.apiError(w, err, "生成采购清单失败")
		return
	}
	checked, err := s.db.GetChecked(menu.ID)
	if err != nil {
		s.apiError(w, err, "读取勾选状态失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"week":    week,
		"groups":  groups,
		"checked": checked,
	})
}

// apiShoppingChecked 保存勾选状态
func (s *Server) apiShoppingChecked(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "只支持 PUT")
		return
	}

	var body struct {
		Week    string          `json:"week"`
		Checked map[string]bool `json:"checked"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}

	_, menu, ok := s.loadMenu(w, body.Week)
	if !ok {
		return
	}
	if err := s.db.SetChecked(menu.ID, body.Checked); err != nil {
		s.apiError(w, err, "保存勾选状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------- 内部工具 ----------

// writeMenuResult 重新读出菜单并返回，顺带带上放宽提示
func (s *Server) writeMenuResult(w http.ResponseWriter, week string, relaxed int, warnings []string) {
	menu, err := s.db.GetMenu(week)
	if err != nil {
		s.apiError(w, err, "读取菜单失败")
		return
	}
	weeks, err := s.db.ListWeeks()
	if err != nil {
		s.apiError(w, err, "读取历史菜单失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"week":     week,
		"menu":     menu,
		"weeks":    weeks,
		"relaxed":  relaxed,
		"warnings": warnings,
	})
}

// loadMenu 取某周菜单，不存在时提示前端先生成
func (s *Server) loadMenu(w http.ResponseWriter, week string) (int64, *database.Menu, bool) {
	if !isValidDate(week) {
		writeError(w, http.StatusBadRequest, "周日期格式应为 YYYY-MM-DD")
		return 0, nil, false
	}

	menu, err := s.db.GetMenu(week)
	if err != nil {
		s.apiError(w, err, "读取菜单失败")
		return 0, nil, false
	}
	if menu == nil || len(menu.Items) == 0 {
		writeError(w, http.StatusNotFound, "这一周还没有菜单，先生成一份吧")
		return 0, nil, false
	}
	return menu.ID, menu, true
}

// splitExisting 把已有菜单拆成生成器要的"已排格子"和"锁定格子"
func splitExisting(menu *database.Menu) (map[generator.Cell]int64, map[generator.Cell]bool) {
	existing := map[generator.Cell]int64{}
	locked := map[generator.Cell]bool{}
	for _, it := range menu.Items {
		cell := generator.Cell{DayIndex: it.DayIndex, Slot: it.Slot, Seq: it.Seq}
		existing[cell] = it.DishID
		if it.Locked {
			locked[cell] = true
		}
	}
	return existing, locked
}

// mealSlots 读取排餐配置（字符串键，便于前端直接渲染）
func (s *Server) mealSlots() map[string][]string {
	slots := map[string][]string{}
	raw := s.db.GetSetting(settingMealSlots, defaultMealSlots)
	if err := json.Unmarshal([]byte(raw), &slots); err != nil {
		utils.GetLogger().Warn("排餐配置解析失败，回退到默认值: %v\n", err)
		_ = json.Unmarshal([]byte(defaultMealSlots), &slots)
	}
	// 保证 7 天都有值，前端不用做兜底
	for day := 0; day < 7; day++ {
		if _, ok := slots[strconv.Itoa(day)]; !ok {
			slots[strconv.Itoa(day)] = []string{}
		}
	}
	return slots
}

// mealSlotsByDay 转成生成器要的整型键
func (s *Server) mealSlotsByDay() map[int][]string {
	out := map[int][]string{}
	for k, v := range s.mealSlots() {
		day, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		out[day] = v
	}
	return out
}

// avoidTags 读取忌口标签
func (s *Server) avoidTags() []string {
	tags := []string{}
	raw := s.db.GetSetting(settingAvoidTags, "[]")
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		utils.GetLogger().Warn("忌口配置解析失败，按无忌口处理: %v\n", err)
		return []string{}
	}
	return tags
}
