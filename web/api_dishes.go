package web

import (
	"net/http"
	"strconv"
	"strings"

	"fanxiaopai/database"
	"fanxiaopai/utils"
)

// settingsKey 需要校验的枚举，新增菜品时同样适用
var (
	validMainIngredients = map[string]bool{
		"鸡": true, "猪": true, "牛": true, "鱼": true, "虾": true,
		"蛋": true, "豆腐": true, "素": true,
	}
	validKinds = map[string]bool{
		database.KindMeat: true, database.KindVeg: true, database.KindSoup: true,
		database.KindStap: true, database.KindQuick: true,
	}
	validSlots = map[string]bool{
		database.SlotBreakfast: true, database.SlotLunch: true, database.SlotDinner: true,
	}
)

// apiDishes 菜品库列表与新增
func (s *Server) apiDishes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listDishes(w, r)
	case http.MethodPost:
		s.createDish(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET / POST")
	}
}

// listDishes 列出菜品，附带全部标签（设置页的忌口选项要用）
func (s *Server) listDishes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	showBlocked := q.Get("blocked") == "1"

	dishes, err := s.db.ListDishes(database.DishFilter{
		Kind:           q.Get("kind"),
		MainIngredient: q.Get("main"),
		Query:          q.Get("q"),
		IncludeBlocked: showBlocked,
	})
	if err != nil {
		s.apiError(w, err, "读取菜品库失败")
		return
	}

	// 全部菜品里出现的标签，作为忌口候选
	all, err := s.db.ListDishes(database.DishFilter{IncludeBlocked: true})
	if err != nil {
		s.apiError(w, err, "读取菜品库失败")
		return
	}
	tags := []string{}
	seen := map[string]bool{}
	for _, d := range all {
		for _, t := range d.Tags {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"dishes": dishes, "tags": tags})
}

// createDish 新增菜品
func (s *Server) createDish(w http.ResponseWriter, r *http.Request) {
	var dish database.Dish
	if err := readJSON(w, r, &dish); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	if msg := validateDish(&dish); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	if err := s.db.CreateDish(&dish); err != nil {
		// 菜名重复是最常见的失败原因，单独提示
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusBadRequest, "已经有同名的一道菜了")
			return
		}
		s.apiError(w, err, "新增菜品失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dish": dish})
}

// apiDish 单道菜的改删，以及拉黑、反馈、补回内置菜
func (s *Server) apiDish(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/dishes/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "缺少菜品 ID")
		return
	}

	if parts[0] == "restore-seed" {
		s.restoreSeed(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "菜品 ID 不合法")
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut:
			s.updateDish(w, r, id)
		case http.MethodDelete:
			s.deleteDish(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "只支持 PUT / DELETE")
		}
		return
	}

	switch parts[1] {
	case "blocked":
		s.setBlocked(w, r, id)
	case "feedback":
		s.addFeedback(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "没有这个操作")
	}
}

// updateDish 编辑菜品
func (s *Server) updateDish(w http.ResponseWriter, r *http.Request, id int64) {
	var dish database.Dish
	if err := readJSON(w, r, &dish); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	dish.ID = id
	if msg := validateDish(&dish); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	if err := s.db.UpdateDish(&dish); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusBadRequest, "已经有同名的一道菜了")
			return
		}
		s.apiError(w, err, "保存菜品失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dish": dish})
}

// deleteDish 删除菜品
func (s *Server) deleteDish(w http.ResponseWriter, r *http.Request, id int64) {
	if err := s.db.DeleteDish(id); err != nil {
		s.apiError(w, err, "删除菜品失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// setBlocked 拉黑 / 恢复
func (s *Server) setBlocked(w http.ResponseWriter, r *http.Request, id int64) {
	if !requirePOST(w, r) {
		return
	}

	var body struct {
		Blocked bool `json:"blocked"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}

	if err := s.db.SetDishBlocked(id, body.Blocked); err != nil {
		s.apiError(w, err, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// addFeedback 记录一次评分：2 爱吃 / 1 一般 / 0 不吃
func (s *Server) addFeedback(w http.ResponseWriter, r *http.Request, id int64) {
	if !requirePOST(w, r) {
		return
	}

	var body struct {
		Rating int    `json:"rating"`
		Date   string `json:"date"`
		MenuID int64  `json:"menu_id"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式有误")
		return
	}
	if body.Rating != database.RatingDislike && body.Rating != database.RatingNormal && body.Rating != database.RatingLike {
		writeError(w, http.StatusBadRequest, "评分只能是 0/1/2")
		return
	}

	if err := s.db.AddFeedback(id, body.MenuID, body.Rating, body.Date); err != nil {
		s.apiError(w, err, "保存评分失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// restoreSeed 补回内置菜品：只补用户库里没有的
func (s *Server) restoreSeed(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	added, err := s.db.RestoreSeedDishes()
	if err != nil {
		s.apiError(w, err, "补回内置菜品失败")
		return
	}
	utils.GetLogger().Info("补回 %d 道内置菜品\n", added)
	writeJSON(w, http.StatusOK, map[string]any{"added": added})
}

// validateDish 校验菜品字段，返回空串表示通过
func validateDish(d *database.Dish) string {
	if strings.TrimSpace(d.Name) == "" {
		return "菜名不能为空"
	}
	if !validMainIngredients[d.MainIngredient] {
		return "主料不合法"
	}
	if !validKinds[d.Kind] {
		return "类型不合法"
	}
	if len(d.Slots) == 0 {
		return "至少要选一个能出现的餐次"
	}
	for _, slot := range d.Slots {
		if !validSlots[slot] {
			return "餐次不合法"
		}
	}
	if d.TimeCost <= 0 {
		return "耗时必须大于 0"
	}
	if len(d.Ingredients) == 0 {
		return "至少要有一个食材，否则采购清单会漏"
	}
	for _, ing := range d.Ingredients {
		if strings.TrimSpace(ing.Name) == "" {
			return "食材名称不能为空"
		}
	}
	return ""
}
