package web

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// apiMealSlots 每天排哪些餐
func (s *Server) apiMealSlots(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"meal_slots": s.mealSlots()})
	case http.MethodPut:
		var body struct {
			MealSlots map[string][]string `json:"meal_slots"`
		}
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "请求格式有误")
			return
		}

		// 只保留合法的餐次，前端传错也不会把配置写坏
		cleaned := map[string][]string{}
		for day := 0; day < 7; day++ {
			key := strconv.Itoa(day)
			slots := []string{}
			for _, slot := range body.MealSlots[key] {
				if validSlots[slot] {
					slots = append(slots, slot)
				}
			}
			cleaned[key] = slots
		}

		raw, err := json.Marshal(cleaned)
		if err != nil {
			s.apiError(w, err, "保存配置失败")
			return
		}
		if err := s.db.SetSetting(settingMealSlots, string(raw)); err != nil {
			s.apiError(w, err, "保存配置失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"meal_slots": cleaned})
	default:
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET / PUT")
	}
}

// apiAvoidTags 忌口标签
func (s *Server) apiAvoidTags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"avoid_tags": s.avoidTags()})
	case http.MethodPut:
		var body struct {
			AvoidTags []string `json:"avoid_tags"`
		}
		if err := readJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "请求格式有误")
			return
		}
		if body.AvoidTags == nil {
			body.AvoidTags = []string{}
		}

		raw, err := json.Marshal(body.AvoidTags)
		if err != nil {
			s.apiError(w, err, "保存配置失败")
			return
		}
		if err := s.db.SetSetting(settingAvoidTags, string(raw)); err != nil {
			s.apiError(w, err, "保存配置失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"avoid_tags": body.AvoidTags})
	default:
		writeError(w, http.StatusMethodNotAllowed, "只支持 GET / PUT")
	}
}
