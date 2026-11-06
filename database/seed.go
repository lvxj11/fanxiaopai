package database

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed seed/dishes.json
var seedFS embed.FS

// 内置菜品的来源标记，与用户自己添加的菜区分
const sourceSeed = "seed"

// seedFile 种子文件的结构
type seedFile struct {
	Dishes []seedDish `json:"dishes"`
}

// seedDish 种子文件里的菜品，字段与 Dish 对齐但不含 ID/权重等运行态字段
type seedDish struct {
	Name           string       `json:"name"`
	MainIngredient string       `json:"main_ingredient"`
	Kind           string       `json:"kind"`
	Slots          []string     `json:"slots"`
	Ingredients    []Ingredient `json:"ingredients"`
	Tags           []string     `json:"tags"`
	TimeCost       int          `json:"time_cost"`
	CanPack        bool         `json:"can_pack"`
	Green          bool         `json:"green"`
	Recipe         string       `json:"recipe"`
	Prep           string       `json:"prep"`
}

// 允许的主料与类型，种子数据必须落在这些枚举内
var (
	validMainIngredients = map[string]bool{
		"鸡": true, "猪": true, "牛": true, "鱼": true, "虾": true,
		"蛋": true, "豆腐": true, "素": true,
	}
	validKinds = map[string]bool{
		KindMeat: true, KindVeg: true, KindSoup: true, KindStap: true, KindQuick: true,
	}
	validSlots = map[string]bool{SlotBreakfast: true, SlotLunch: true, SlotDinner: true}
)

// ImportSeedDishes 导入内置菜品，返回实际导入条数。
// 由首次 /setup 在用户勾选「导入内置菜」时调用；启动流程不再自动导入。
// 幂等：只有空表才全量导入，之后一条都不写——用户删过的内置菜、改过的做法/备料都不会被动。
// 现存开发库已有 122 道菜，调用它会安全地成为 no-op。
func (d *Database) ImportSeedDishes() (int, error) {
	seed, err := parseSeed()
	if err != nil {
		return 0, err
	}

	var count int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM dishes").Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for _, sd := range seed.Dishes {
		if _, err := tx.Exec(`INSERT INTO dishes
            (name, main_ingredient, kind, slots, ingredients, tags, time_cost, can_pack, green, blocked, weight, source, created_at, recipe, prep)
            VALUES (?,?,?,?,?,?,?,?,?,0,1.0,?,?,?,?)`,
			sd.Name, sd.MainIngredient, sd.Kind, marshalList(sd.Slots), marshalIngredients(sd.Ingredients),
			marshalList(sd.Tags), sd.TimeCost, boolToInt(sd.CanPack), boolToInt(sd.Green),
			sourceSeed, "", sd.Recipe, sd.Prep); err != nil {
			return 0, fmt.Errorf("导入内置菜品 %s 失败: %v", sd.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(seed.Dishes), nil
}

// RestoreSeedDishes 显式补回内置菜品：只补用户库里没有的，已存在的字段一律不动
func (d *Database) RestoreSeedDishes() (int, error) {
	seed, err := parseSeed()
	if err != nil {
		return 0, err
	}

	existing := map[string]bool{}
	rows, err := d.db.Query("SELECT name FROM dishes")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	added := 0
	for _, sd := range seed.Dishes {
		if existing[sd.Name] {
			continue
		}
		dish := &Dish{
			Name:           sd.Name,
			MainIngredient: sd.MainIngredient,
			Kind:           sd.Kind,
			Slots:          sd.Slots,
			Ingredients:    sd.Ingredients,
			Tags:           sd.Tags,
			TimeCost:       sd.TimeCost,
			CanPack:        sd.CanPack,
			Green:          sd.Green,
			Recipe:         sd.Recipe,
			Prep:           sd.Prep,
			Source:         sourceSeed,
		}
		if err := d.CreateDish(dish); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// parseSeed 读取并校验种子文件。坏数据直接报错退出，避免带着脏菜库运行
func parseSeed() (*seedFile, error) {
	raw, err := seedFS.ReadFile("seed/dishes.json")
	if err != nil {
		return nil, fmt.Errorf("读取内置菜品文件失败: %v", err)
	}

	var seed seedFile
	if err := json.Unmarshal(raw, &seed); err != nil {
		return nil, fmt.Errorf("解析内置菜品文件失败: %v", err)
	}

	names := map[string]bool{}
	for i, sd := range seed.Dishes {
		where := fmt.Sprintf("第 %d 道菜（%s）", i+1, sd.Name)
		if sd.Name == "" {
			return nil, fmt.Errorf("%s: 缺少菜名", where)
		}
		if names[sd.Name] {
			return nil, fmt.Errorf("%s: 菜名重复", where)
		}
		names[sd.Name] = true

		if !validMainIngredients[sd.MainIngredient] {
			return nil, fmt.Errorf("%s: 主料 %q 不在允许范围内", where, sd.MainIngredient)
		}
		if !validKinds[sd.Kind] {
			return nil, fmt.Errorf("%s: 类型 %q 不在允许范围内", where, sd.Kind)
		}
		if len(sd.Slots) == 0 {
			return nil, fmt.Errorf("%s: 至少要能出现在一个餐次", where)
		}
		for _, s := range sd.Slots {
			if !validSlots[s] {
				return nil, fmt.Errorf("%s: 餐次 %q 不合法", where, s)
			}
		}
		if len(sd.Ingredients) == 0 {
			return nil, fmt.Errorf("%s: 至少要有一个食材，否则采购清单会漏", where)
		}
		if sd.TimeCost <= 0 {
			return nil, fmt.Errorf("%s: 耗时必须为正", where)
		}
		if strings.TrimSpace(sd.Kind) == KindVeg && sd.Green && sd.MainIngredient != "素" {
			return nil, fmt.Errorf("%s: 绿叶菜的主料应为素", where)
		}
	}

	return &seed, nil
}
