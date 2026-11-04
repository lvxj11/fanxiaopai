package database

// 餐次：一天内可能出现的三个餐
const (
	SlotBreakfast = "breakfast"
	SlotLunch     = "lunch"
	SlotDinner    = "dinner"
)

// 菜品类型。生成器按"格内角色"选菜，每个角色只接受特定 kind
const (
	KindMeat  = "荤菜" // 正餐的荤菜
	KindVeg   = "素菜" // 正餐的素菜
	KindSoup  = "汤"
	KindStap  = "主食" // 米饭面条馒头等，也用作早餐的碳水
	KindQuick = "快手" // 10~15 分钟内能上桌的，主要用作早餐的蛋白
)

// 反馈评分
const (
	RatingDislike = 0 // 不吃：直接拉黑
	RatingNormal  = 1 // 一般
	RatingLike    = 2 // 爱吃
)

// Ingredient 菜用到的一种食材。Cat 决定采购清单里的分组
type Ingredient struct {
	Name string `json:"name"`
	Cat  string `json:"cat"`
	Qty  string `json:"qty"`
}

// Dish 菜品
// Slots / Ingredients / Tags 是多值字段，库里存 JSON 文本列：
// 生成器每次把全表读进内存做加权抽样，写入只发生在用户增删改，
// 拆成关联表只会让读写都变复杂，而反查场景（哪些菜用鸡蛋）本机用不上
type Dish struct {
	ID             int64        `json:"id"`
	Name           string       `json:"name"`
	MainIngredient string       `json:"main_ingredient"`
	Kind           string       `json:"kind"`
	Slots          []string     `json:"slots"`
	Ingredients    []Ingredient `json:"ingredients"`
	Tags           []string     `json:"tags"`
	TimeCost       int          `json:"time_cost"`
	CanPack        bool         `json:"can_pack"`
	Green          bool         `json:"green"`
	Blocked        bool         `json:"blocked"`
	Weight         float64      `json:"weight"`
	Source         string       `json:"source"`
	CreatedAt      string       `json:"created_at"`
	LastMade       string       `json:"last_made,omitempty"`
	Recipe         string       `json:"recipe"`
	Prep           string       `json:"prep"`
}

// MenuItem 菜单里的一格（某天某餐的第 seq 道菜）
type MenuItem struct {
	ID       int64  `json:"id"`
	MenuID   int64  `json:"menu_id"`
	DayIndex int    `json:"day_index"`
	Slot     string `json:"slot"`
	Seq      int    `json:"seq"`
	DishID   int64  `json:"dish_id"`
	Locked   bool   `json:"locked"`
	Dish     *Dish  `json:"dish,omitempty"`
}

// Menu 一周菜单
type Menu struct {
	ID        int64      `json:"id"`
	WeekStart string     `json:"week_start"`
	Items     []MenuItem `json:"items"`
}

// ShoppingItem 采购清单里的一行
type ShoppingItem struct {
	Name string `json:"name"`
	Qty  string `json:"qty"`
}

// ShoppingGroup 按食材分类聚合后的采购清单分组
type ShoppingGroup struct {
	Cat   string         `json:"cat"`
	Items []ShoppingItem `json:"items"`
}

// DishFilter 菜品库列表筛选条件
type DishFilter struct {
	Kind           string
	MainIngredient string
	Query          string
	IncludeBlocked bool
}
