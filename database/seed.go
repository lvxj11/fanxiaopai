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
	Version int        `json:"version"`
	Dishes  []seedDish `json:"dishes"`
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

// loadSeed 加载内置菜品
// 判据是"表是否空"和"种子版本"，而不是种子文件在不在：
//   - 空表：全量导入（首次启动）
//   - 非空且版本未变：一条都不写，用户删过的内置菜重启不会回来
//   - 非空但版本变了：按菜名增量补缺，已存在的一律不动（日后扩充菜库用）
func (d *Database) loadSeed() error {
	seed, err := parseSeed()
	if err != nil {
		return err
	}

	var count int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM dishes").Scan(&count); err != nil {
		return err
	}

	storedVersion := d.GetSetting("seed_version", "")
	currentVersion := fmt.Sprintf("%d", seed.Version)

	if count > 0 && storedVersion == currentVersion {
		return nil
	}

	existing := map[string]bool{}
	rows, err := d.db.Query("SELECT name FROM dishes")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	added := 0
	for _, sd := range seed.Dishes {
		if existing[sd.Name] {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO dishes
            (name, main_ingredient, kind, slots, ingredients, tags, time_cost, can_pack, green, blocked, weight, source, created_at)
            VALUES (?,?,?,?,?,?,?,?,?,0,1.0,?,?)`,
			sd.Name, sd.MainIngredient, sd.Kind, marshalList(sd.Slots), marshalIngredients(sd.Ingredients),
			marshalList(sd.Tags), sd.TimeCost, boolToInt(sd.CanPack), boolToInt(sd.Green),
			sourceSeed, ""); err != nil {
			return fmt.Errorf("导入内置菜品 %s 失败: %v", sd.Name, err)
		}
		added++
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if err := d.SetSetting("seed_version", currentVersion); err != nil {
		return err
	}

	if added > 0 {
		fmt.Printf("已导入 %d 道内置菜品\n", added)
	}
	return nil
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
