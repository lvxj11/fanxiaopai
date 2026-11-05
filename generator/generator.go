package generator

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"time"

	"fanxiaopai/database"
)

// ErrNoDishes 菜品库为空，没法生成
var ErrNoDishes = errors.New("菜品库里没有可用的菜，先去菜库添加几道吧")

// Cell 菜单里的一格：某天某餐的第 seq 道菜
type Cell struct {
	DayIndex int
	Slot     string
	Seq      int
}

// Request 一次生成请求
type Request struct {
	WeekStart string           // 周一的日期，仅用于计算"最近做过"
	MealSlots map[int][]string // day_index -> 该天要排的餐次
	Dishes    []database.Dish  // 候选菜（生成器自行过滤黑名单与忌口）
	AvoidTags []string         // 忌口标签
	Existing  map[Cell]int64   // 当前已排好的格子，用于保留锁定/其他格
	Locked    map[Cell]bool    // 锁定的格子，不参与重排
	Target    *Cell            // 非空时只重排这一格（单格换菜）
	Now       time.Time        // 计算"久未做"用，测试里可固定
}

// Item 生成结果里的一格
type Item struct {
	Cell
	DishID int64
}

// Result 生成结果
type Result struct {
	Items    []Item
	Relaxed  int      // 放宽到第几级（0 表示完全满足约束）
	Warnings []string // 放宽了什么，直接展示给用户
}

// Generator 生成器抽象。目前只有本地加权随机一种实现，
// 以后接 AI 只要再实现一个，调用方不用改
type Generator interface {
	Generate(ctx context.Context, req Request) (*Result, error)
}

// ---------- 角色与耗时 ----------

// 每一格里每道菜扮演的角色，决定它从哪个池子里选
type role struct {
	name  string
	kinds []string
}

// slotRoles 早餐 = 碳水 + 蛋白；正餐 = 荤 + 素 + 汤/主食
var slotRoles = map[string][]role{
	database.SlotBreakfast: {
		{name: "carb", kinds: []string{database.KindStap}},
		{name: "protein", kinds: []string{database.KindQuick}},
	},
	database.SlotLunch: {
		{name: "meat", kinds: []string{database.KindMeat}},
		{name: "veg", kinds: []string{database.KindVeg}},
		{name: "side", kinds: []string{database.KindSoup, database.KindStap}},
	},
	database.SlotDinner: {
		{name: "meat", kinds: []string{database.KindMeat}},
		{name: "veg", kinds: []string{database.KindVeg}},
		{name: "side", kinds: []string{database.KindSoup, database.KindStap}},
	},
}

// fallbackKinds 主池选不出来时的借用顺序（降级兜底，不是常规路径）
var fallbackKinds = map[string][]string{
	"carb":    {database.KindQuick, database.KindVeg},
	"protein": {database.KindVeg, database.KindSoup},
	"meat":    {database.KindVeg, database.KindQuick},
	"veg":     {database.KindQuick, database.KindSoup},
	"side":    {database.KindQuick, database.KindVeg},
}

// 耗时上限：早上赶时间，工作日晚上也不能太久，周末可以慢炖
const (
	timeLimitBreakfast = 20
	timeLimitWeekday   = 35
	timeLimitWeekend   = 90
)

func isWeekend(dayIndex int) bool { return dayIndex >= 5 }

func timeLimit(slot string, dayIndex int) int {
	if slot == database.SlotBreakfast {
		return timeLimitBreakfast
	}
	if isWeekend(dayIndex) {
		return timeLimitWeekend
	}
	return timeLimitWeekday
}

// ---------- 降级阶梯 ----------

// relax 某一级放宽策略
type relax struct {
	level      int
	greenDaily bool    // 每天都要有绿叶菜
	weekUnique bool    // 整周不重复（false 时放宽为间隔 ≥2 天）
	gap48      bool    // 荤菜主料 48 小时内不重复
	timeMul    float64 // 耗时上限倍数，0 表示忽略耗时
}

// relaxLevels 从严格到宽松。菜品库偏小时逐级退让，
// 保证一定能给出结果，而不是卡死在互相矛盾的约束里
var relaxLevels = []relax{
	{level: 0, greenDaily: true, weekUnique: true, gap48: true, timeMul: 1},
	{level: 1, greenDaily: false, weekUnique: true, gap48: true, timeMul: 1},
	{level: 2, greenDaily: false, weekUnique: false, gap48: true, timeMul: 1},
	{level: 3, greenDaily: false, weekUnique: false, gap48: false, timeMul: 1.5},
	{level: 4, greenDaily: false, weekUnique: false, gap48: false, timeMul: 0},
}

// ---------- 本地加权随机实现 ----------

// LocalWeighted 基于菜品库和约束做加权随机，不依赖任何外部服务
type LocalWeighted struct {
	rng *rand.Rand
}

// NewLocalWeighted 创建本地生成器，rng 由外部注入便于测试固定随机序列
func NewLocalWeighted(rng *rand.Rand) *LocalWeighted {
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &LocalWeighted{rng: rng}
}

// Generate 生成一周菜单
func (g *LocalWeighted) Generate(ctx context.Context, req Request) (*Result, error) {
	if len(req.Dishes) == 0 {
		return nil, ErrNoDishes
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}

	cells := requestCells(req)
	if len(cells) == 0 {
		return &Result{Items: keepExisting(req)}, nil
	}

	// existing 里凡是本次要重排的格子（cells），都交给下面的生成循环重新产出；
	// 只把"不用重排的格子"留下，否则 tryPlan 会先原样计入、再生成一次，
	// 同一格在结果里出现两条，保存时撞 menu_items 的唯一约束。
	inCells := make(map[Cell]bool, len(cells))
	for _, c := range cells {
		inCells[c] = true
	}
	kept := make(map[Cell]int64, len(req.Existing))
	for cell, dishID := range req.Existing {
		if !inCells[cell] {
			kept[cell] = dishID
		}
	}
	req.Existing = kept

	deadline := req.Now.Add(200 * time.Millisecond)
	best := []Item{}

	for _, lv := range relaxLevels {
		for attempt := 0; attempt < 6; attempt++ {
			plan, ok := g.tryPlan(req, cells, lv)
			if len(plan) > len(best) {
				best = plan
			}
			if ok {
				return &Result{Items: plan, Relaxed: lv.level, Warnings: warningsFor(lv)}, nil
			}
			if time.Now().After(deadline) {
				break
			}
		}
	}

	// 所有级别都排不满：做一轮兜底，能填多少填多少，明确告诉用户放宽了什么
	fallback := g.fillBestEffort(req, cells)
	if len(fallback) > len(best) {
		best = fallback
	}
	warnings := warningsFor(relaxLevels[len(relaxLevels)-1])
	if len(best) < len(cells) {
		warnings = append(warnings, "有格子没排上菜，菜库里符合条件的菜不够，去菜库加几道吧")
	}
	return &Result{Items: best, Relaxed: len(relaxLevels) - 1, Warnings: warnings}, nil
}

// requestCells 展开本次要排的所有格子；单格换菜时只有目标格
func requestCells(req Request) []Cell {
	if req.Target != nil {
		return []Cell{*req.Target}
	}

	cells := []Cell{}
	for day := 0; day < 7; day++ {
		slots := req.MealSlots[day]
		sort.Slice(slots, func(i, j int) bool { return slotOrder(slots[i]) < slotOrder(slots[j]) })
		for _, slot := range slots {
			for seq := range slotRoles[slot] {
				cell := Cell{DayIndex: day, Slot: slot, Seq: seq}
				if req.Locked[cell] {
					continue
				}
				cells = append(cells, cell)
			}
		}
	}
	return cells
}

func slotOrder(slot string) int {
	switch slot {
	case database.SlotBreakfast:
		return 0
	case database.SlotLunch:
		return 1
	default:
		return 2
	}
}

// keepExisting 没有可排的格子时（比如整周都锁定了）原样返回
func keepExisting(req Request) []Item {
	items := []Item{}
	for cell, dishID := range req.Existing {
		items = append(items, Item{Cell: cell, DishID: dishID})
	}
	sortItems(items)
	return items
}

// ---------- 单次尝试 ----------

// state 一次排班过程中的中间状态
type state struct {
	used    map[int64]bool
	lastDay map[int64]int
	mainDay map[string]int // 荤菜主料 -> 上次出现的天
	green   map[int]bool   // 该天是否已有绿叶菜
	seafood int
}

func newState() *state {
	return &state{
		used:    map[int64]bool{},
		lastDay: map[int64]int{},
		mainDay: map[string]int{},
		green:   map[int]bool{},
	}
}

// tryPlan 按给定放宽级别尝试排出一整周
func (g *LocalWeighted) tryPlan(req Request, cells []Cell, lv relax) ([]Item, bool) {
	st := newState()
	items := []Item{}

	// 先把不用重排的格子（锁定格 / 单格换菜时的其他格）计入状态
	for cell, dishID := range req.Existing {
		if req.Target != nil && cell == *req.Target {
			continue // 要换掉的那一格不算已用，否则永远选不出新菜
		}
		items = append(items, Item{Cell: cell, DishID: dishID})
		if dish := findDish(req.Dishes, dishID); dish != nil {
			st.place(*dish, cell.DayIndex)
		}
	}

	// MRV：候选池最小的格先填，先啃硬骨头，减少排到最后没菜可用的概率
	ordered := g.orderCells(req, cells, lv, st)

	for _, cell := range ordered {
		dish, ok := g.pickFor(req, cell, lv, st)
		if !ok {
			return items, false
		}
		items = append(items, Item{Cell: cell, DishID: dish.ID})
		st.place(*dish, cell.DayIndex)
	}

	// 单格换菜不做整周检查：其余格子是用户已经确认过的，不该被它推翻
	if req.Target == nil && !weeklyCheck(items, req, lv) {
		return items, false
	}

	sortItems(items)
	return items, true
}

// orderCells 按候选数量升序排列（MRV）
func (g *LocalWeighted) orderCells(req Request, cells []Cell, lv relax, st *state) []Cell {
	counts := make([]int, len(cells))
	for i, cell := range cells {
		counts[i] = len(g.candidates(req, cell, lv, st))
	}
	ordered := make([]Cell, len(cells))
	copy(ordered, cells)
	sort.SliceStable(ordered, func(i, j int) bool { return counts[i] < counts[j] })
	return ordered
}

// pickFor 为某一格选一道菜，找不到返回 false
func (g *LocalWeighted) pickFor(req Request, cell Cell, lv relax, st *state) (*database.Dish, bool) {
	cands := g.candidates(req, cell, lv, st)
	if len(cands) == 0 {
		return nil, false
	}

	weights := make([]float64, len(cands))
	total := 0.0
	for i, d := range cands {
		w := g.weightFor(req, d, cell, st)
		if w < 0 {
			w = 0
		}
		weights[i] = w
		total += w
	}
	if total <= 0 {
		return &cands[g.rng.Intn(len(cands))], true
	}

	r := g.rng.Float64() * total
	for i, w := range weights {
		r -= w
		if r <= 0 {
			return &cands[i], true
		}
	}
	return &cands[len(cands)-1], true
}

// candidates 列出某一格在当前状态下可选的菜。
// 正常按角色取池，取空时按 fallbackKinds 借用相邻池
func (g *LocalWeighted) candidates(req Request, cell Cell, lv relax, st *state) []database.Dish {
	roles := slotRoles[cell.Slot]
	if cell.Seq >= len(roles) {
		return nil
	}
	r := roles[cell.Seq]

	out := g.filterPool(req, cell, lv, st, r.kinds)
	if len(out) == 0 {
		out = g.filterPool(req, cell, lv, st, fallbackKinds[r.name])
	}
	return out
}

// filterPool 按硬约束过滤候选
func (g *LocalWeighted) filterPool(req Request, cell Cell, lv relax, st *state, kinds []string) []database.Dish {
	limit := 0
	if lv.timeMul > 0 {
		limit = int(float64(timeLimit(cell.Slot, cell.DayIndex)) * lv.timeMul)
	}

	out := []database.Dish{}
	for _, d := range req.Dishes {
		if d.Blocked || !hasSlot(d, cell.Slot) || !hasKind(d, kinds) {
			continue
		}
		if limit > 0 && d.TimeCost > limit {
			continue
		}
		if hasAnyTag(d, req.AvoidTags) {
			continue
		}
		if st.used[d.ID] {
			continue
		}
		if !lv.weekUnique {
			// 放宽为"间隔至少 2 天"
			if last, ok := st.lastDay[d.ID]; ok && abs(last-cell.DayIndex) < 2 {
				continue
			}
		}
		if lv.gap48 && d.Kind == database.KindMeat {
			if last, ok := st.mainDay[d.MainIngredient]; ok && abs(last-cell.DayIndex) < 2 {
				continue
			}
		}
		// 要求每天有绿叶菜时，素菜格优先只从绿叶菜里选
		if lv.greenDaily && isVegRole(cell, d) && !st.green[cell.DayIndex] {
			if !d.Green && st.hasGreenAvailable(req, cell) {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

// hasGreenAvailable 该天这一格还有绿叶菜可选时才强制绿叶菜，避免把格子逼空
func (st *state) hasGreenAvailable(req Request, cell Cell) bool {
	for _, d := range req.Dishes {
		if d.Blocked || !hasSlot(d, cell.Slot) || d.Kind != database.KindVeg || !d.Green {
			continue
		}
		if st.used[d.ID] {
			continue
		}
		return true
	}
	return false
}

func isVegRole(cell Cell, d database.Dish) bool {
	roles := slotRoles[cell.Slot]
	if cell.Seq >= len(roles) {
		return false
	}
	return roles[cell.Seq].name == "veg" && d.Kind == database.KindVeg
}

// weightFor 计算某道菜在这一格的权重：反馈 × 久未做 × 软约束罚分
func (g *LocalWeighted) weightFor(req Request, d database.Dish, cell Cell, st *state) float64 {
	w := d.Weight
	if w <= 0 {
		w = 1.0
	}

	// 久未做的菜适当提权，让菜单换得开
	switch {
	case d.LastMade == "":
		w *= 1.2
	default:
		if days, err := daysBetween(d.LastMade, req.Now); err == nil {
			switch {
			case days <= 7:
				w *= 0.7
			case days <= 14:
				w *= 1.0
			default:
				w *= 1.3
			}
		}
	}

	// 鱼虾一周至少 2 次：周中之后还没出现过就提权
	if d.Kind == database.KindMeat && (d.MainIngredient == "鱼" || d.MainIngredient == "虾") {
		needed := 2 - st.seafood
		if needed > 0 && cell.DayIndex >= 7-needed {
			w *= 1.8
		}
	}

	// 当天还缺绿叶菜时，绿叶菜本身提权（greenDaily 已硬性过滤，这里覆盖放宽后的级别）
	if d.Green && !st.green[cell.DayIndex] {
		w *= 1.5
	}

	return w
}

// fillBestEffort 兜底：忽略所有软约束，能填几格填几格
func (g *LocalWeighted) fillBestEffort(req Request, cells []Cell) []Item {
	mostRelaxed := relax{level: 9, timeMul: 0}
	st := newState()
	items := []Item{}

	for cell, dishID := range req.Existing {
		if req.Target != nil && cell == *req.Target {
			continue
		}
		items = append(items, Item{Cell: cell, DishID: dishID})
		if dish := findDish(req.Dishes, dishID); dish != nil {
			st.used[dish.ID] = true
		}
	}

	for _, cell := range cells {
		if d, ok := g.pickFor(req, cell, mostRelaxed, st); ok {
			items = append(items, Item{Cell: cell, DishID: d.ID})
			st.place(*d, cell.DayIndex)
		}
	}

	sortItems(items)
	return items
}

// weeklyCheck 整周层面的检查，未通过就换一次随机重排
func weeklyCheck(items []Item, req Request, lv relax) bool {
	if lv.level > 1 {
		return true
	}

	greenDays := map[int]bool{}
	seafood := 0
	for _, it := range items {
		d := findDish(req.Dishes, it.DishID)
		if d == nil {
			continue
		}
		if d.Green {
			greenDays[it.DayIndex] = true
		}
		if d.MainIngredient == "鱼" || d.MainIngredient == "虾" {
			seafood++
		}
	}

	if seafood < 2 && seafoodAvailable(req) >= 2 {
		return false
	}
	if lv.greenDaily {
		for day := 0; day < 7; day++ {
			if hasMainMeal(req, day) && !greenDays[day] {
				return false
			}
		}
	}
	return true
}

// seafoodAvailable 还能用的鱼虾数量。
// 一道都不剩时（全被拉黑或全在忌口里）就不该再要求"每周至少 2 次鱼虾"，
// 否则每次生成都会因为这条硬要求失败而白白放宽其他约束
func seafoodAvailable(req Request) int {
	n := 0
	for _, d := range req.Dishes {
		if d.Blocked || hasAnyTag(d, req.AvoidTags) {
			continue
		}
		if d.MainIngredient == "鱼" || d.MainIngredient == "虾" {
			n++
		}
	}
	return n
}

func hasMainMeal(req Request, day int) bool {
	for _, slot := range req.MealSlots[day] {
		if slot == database.SlotLunch || slot == database.SlotDinner {
			return true
		}
	}
	return false
}

func warningsFor(lv relax) []string {
	if lv.level == 0 {
		return nil
	}
	warnings := []string{}
	if !lv.greenDaily {
		warnings = append(warnings, "绿叶菜不再保证每天都有")
	}
	if !lv.weekUnique {
		warnings = append(warnings, "同一道菜一周内可能重复出现")
	}
	if !lv.gap48 {
		warnings = append(warnings, "连续两天可能吃到同一种主料")
	}
	if lv.timeMul != 1 {
		warnings = append(warnings, "放宽了单道菜的耗时限制")
	}
	return warnings
}

// ---------- 状态维护与小工具 ----------

func (st *state) place(d database.Dish, day int) {
	st.used[d.ID] = true
	st.lastDay[d.ID] = day
	if d.Kind == database.KindMeat {
		st.mainDay[d.MainIngredient] = day
	}
	if d.Green {
		st.green[day] = true
	}
	if d.MainIngredient == "鱼" || d.MainIngredient == "虾" {
		st.seafood++
	}
}

func findDish(dishes []database.Dish, id int64) *database.Dish {
	for i := range dishes {
		if dishes[i].ID == id {
			return &dishes[i]
		}
	}
	return nil
}

func hasSlot(d database.Dish, slot string) bool {
	for _, s := range d.Slots {
		if s == slot {
			return true
		}
	}
	return false
}

func hasKind(d database.Dish, kinds []string) bool {
	for _, k := range kinds {
		if d.Kind == k {
			return true
		}
	}
	return false
}

func hasAnyTag(d database.Dish, tags []string) bool {
	for _, t := range tags {
		for _, dt := range d.Tags {
			if dt == t {
				return true
			}
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func daysBetween(date string, now time.Time) (int, error) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, err
	}
	return int(now.Sub(t).Hours() / 24), nil
}

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].DayIndex != items[j].DayIndex {
			return items[i].DayIndex < items[j].DayIndex
		}
		if items[i].Slot != items[j].Slot {
			return slotOrder(items[i].Slot) < slotOrder(items[j].Slot)
		}
		return items[i].Seq < items[j].Seq
	})
}
