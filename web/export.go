package web

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"time"

	"fanxiaopai/database"
	"fanxiaopai/utils"
)

// 快照页没有 JS，下面几个名字只能在这里重算一遍，
// 取值要和 menu.html / shopping.html 里前端算的保持一致
var (
	exportDayNames = []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

	exportSlotNames = map[string]string{
		database.SlotBreakfast: "早餐",
		database.SlotLunch:     "午餐",
		database.SlotDinner:    "晚餐",
	}

	exportCatEmoji = map[string]string{
		"肉蛋": "🥩", "水产": "🐟", "蔬菜": "🥬", "豆制品": "🧈",
		"米面": "🍚", "调料": "🧂", "其他": "🛒",
	}
)

// ---------- 视图数据 ----------

type exportSlot struct {
	Label string
	Items []database.MenuItem
}

type exportDay struct {
	Label string
	Date  string
	Slots []exportSlot
}

// exportCheck 采购清单里的一行，Checked 决定快照里是否预先划掉
type exportCheck struct {
	Name    string
	Qty     string
	Checked bool
}

type exportGroup struct {
	Cat   string
	Emoji string
	Items []exportCheck
}

type exportData struct {
	Title      string
	RangeLabel string
	Week       string
	HasMenu    bool
	Days       []exportDay
	Groups     []exportGroup
	Total      int
	Version    string
	// CSS 是整份预编译样式，内联进 <style>，快照因此不依赖任何外部文件
	CSS template.CSS
}

// ---------- 处理函数 ----------

// exportSnapshot 导出一周菜单 + 采购清单的自包含快照。
// 样式和数据全部内联，存到手机上断网也能打开，不需要 HTTPS，也不需要 Service Worker。
func (s *Server) exportSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "只支持 GET", http.StatusMethodNotAllowed)
		return
	}

	css, err := staticFiles.ReadFile("static/app.css")
	if err != nil {
		utils.GetLogger().Error("读取样式文件失败: %v\n", err)
		http.Error(w, "读取样式失败", http.StatusInternalServerError)
		return
	}

	week := weekParam(r)
	data := exportData{
		Title:      "离线快照",
		Week:       week,
		RangeLabel: week + " 那一周",
		Version:    utils.Version,
		CSS:        template.CSS(css),
	}

	menu, err := s.db.GetMenu(week)
	if err != nil {
		utils.GetLogger().Error("读取菜单失败: %v\n", err)
		http.Error(w, "读取菜单失败", http.StatusInternalServerError)
		return
	}

	if menu != nil && len(menu.Items) > 0 {
		data.Days = exportDays(week, menu, s.mealSlotsByDay())

		groups, err := s.db.ShoppingList(menu.ID)
		if err != nil {
			utils.GetLogger().Error("生成采购清单失败: %v\n", err)
			http.Error(w, "生成采购清单失败", http.StatusInternalServerError)
			return
		}
		checked, err := s.db.GetChecked(menu.ID)
		if err != nil {
			utils.GetLogger().Error("读取勾选状态失败: %v\n", err)
			http.Error(w, "读取勾选状态失败", http.StatusInternalServerError)
			return
		}
		data.Groups = exportGroups(groups, checked)
		data.HasMenu = true
		for _, g := range data.Groups {
			data.Total += len(g.Items)
		}
	}

	tmpl, err := template.ParseFS(templateFiles, "templates/export.html")
	if err != nil {
		utils.GetLogger().Error("解析快照模板失败: %v\n", err)
		http.Error(w, "解析模板失败", http.StatusInternalServerError)
		return
	}

	// 响应头必须在第一次 Write 之前设好
	if r.URL.Query().Get("download") == "1" {
		name := "饭小排-" + week + ".html"
		w.Header().Set("Content-Disposition", fmt.Sprintf(
			`attachment; filename="fanxiaopai-%s.html"; filename*=UTF-8''%s`,
			week, url.PathEscape(name)))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := tmpl.Execute(w, data); err != nil {
		utils.GetLogger().Error("渲染离线快照失败: %v\n", err)
	}
}

// exportDays 把菜单按天和餐次排好，日期由周一推算
func exportDays(week string, menu *database.Menu, slotsByDay map[int][]string) []exportDay {
	monday, err := time.Parse("2006-01-02", week)
	if err != nil {
		return nil
	}

	days := make([]exportDay, 0, 7)
	for i := 0; i < 7; i++ {
		day := exportDay{
			Label: exportDayNames[i],
			Date:  monday.AddDate(0, 0, i).Format("1月2日"),
		}
		for _, slot := range slotsByDay[i] {
			items := make([]database.MenuItem, 0, 2)
			for _, it := range menu.Items {
				if it.DayIndex == i && it.Slot == slot {
					items = append(items, it)
				}
			}
			sort.Slice(items, func(a, b int) bool { return items[a].Seq < items[b].Seq })
			if len(items) == 0 {
				continue
			}
			label, ok := exportSlotNames[slot]
			if !ok {
				label = slot
			}
			day.Slots = append(day.Slots, exportSlot{Label: label, Items: items})
		}
		days = append(days, day)
	}
	return days
}

// exportGroups 食材分组已在 db.ShoppingList 里算好，这里只补 emoji 和勾选状态
func exportGroups(groups []database.ShoppingGroup, checked map[string]bool) []exportGroup {
	out := make([]exportGroup, 0, len(groups))
	for _, g := range groups {
		emoji, ok := exportCatEmoji[g.Cat]
		if !ok {
			emoji = "🛒"
		}
		eg := exportGroup{Cat: g.Cat, Emoji: emoji}
		for _, it := range g.Items {
			eg.Items = append(eg.Items, exportCheck{
				Name:    it.Name,
				Qty:     it.Qty,
				Checked: checked[g.Cat+"|"+it.Name],
			})
		}
		out = append(out, eg)
	}
	return out
}
