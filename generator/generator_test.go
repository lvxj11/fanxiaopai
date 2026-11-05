package generator

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"fanxiaopai/database"
)

// 默认的排餐配置：工作日早晚，周末三餐
func defaultMealSlots() map[int][]string {
	slots := map[int][]string{}
	for day := 0; day < 7; day++ {
		if day >= 5 {
			slots[day] = []string{database.SlotBreakfast, database.SlotLunch, database.SlotDinner}
		} else {
			slots[day] = []string{database.SlotBreakfast, database.SlotDinner}
		}
	}
	return slots
}

// makeDish 造一道测试用菜
func makeDish(id int64, name, main, kind string, slots []string, timeCost int) database.Dish {
	return database.Dish{
		ID:             id,
		Name:           name,
		MainIngredient: main,
		Kind:           kind,
		Slots:          slots,
		TimeCost:       timeCost,
		Weight:         1.0,
	}
}

// bigLibrary 造一个比例接近真实种子库的菜库
func bigLibrary() []database.Dish {
	dishes := []database.Dish{}
	id := int64(1)
	lunchDinner := []string{database.SlotLunch, database.SlotDinner}

	mains := []string{"鸡", "猪", "牛", "鱼", "虾", "豆腐"}
	for _, main := range mains {
		for i := 0; i < 6; i++ {
			dishes = append(dishes, makeDish(id, main+"菜"+string(rune('A'+i)), main, database.KindMeat, lunchDinner, 20+i*5))
			id++
		}
	}
	for i := 0; i < 12; i++ { // 绿叶菜
		d := makeDish(id, "绿叶菜"+string(rune('A'+i)), "素", database.KindVeg, lunchDinner, 10)
		d.Green = true
		dishes = append(dishes, d)
		id++
	}
	for i := 0; i < 18; i++ { // 非绿叶素菜
		dishes = append(dishes, makeDish(id, "素菜"+string(rune('A'+i)), "素", database.KindVeg, lunchDinner, 15))
		id++
	}
	for i := 0; i < 15; i++ { // 汤
		dishes = append(dishes, makeDish(id, "汤"+string(rune('A'+i)), "素", database.KindSoup, lunchDinner, 25))
		id++
	}
	for i := 0; i < 12; i++ { // 早餐碳水
		dishes = append(dishes, makeDish(id, "早餐碳水"+string(rune('A'+i)), "素", database.KindStap, []string{database.SlotBreakfast}, 10))
		id++
	}
	for i := 0; i < 12; i++ { // 早餐蛋白
		dishes = append(dishes, makeDish(id, "早餐蛋白"+string(rune('A'+i)), "蛋", database.KindQuick, []string{database.SlotBreakfast}, 10))
		id++
	}
	return dishes
}

func generate(t *testing.T, req Request) *Result {
	t.Helper()
	g := NewLocalWeighted(rand.New(rand.NewSource(42)))
	res, err := g.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	return res
}

func TestGenerateFillsEveryCell(t *testing.T) {
	res := generate(t, Request{MealSlots: defaultMealSlots(), Dishes: bigLibrary()})

	// 早餐 2 格 × 7 天 + 正餐 3 格 × 9 顿（5 天晚餐 + 2 天午晚餐）
	want := 7*2 + 9*3
	if len(res.Items) != want {
		t.Fatalf("格子数不对: got %d, want %d", len(res.Items), want)
	}
	if res.Relaxed != 0 {
		t.Fatalf("菜库充足时不应放宽约束，实际放宽到第 %d 级，警告: %v", res.Relaxed, res.Warnings)
	}
}

func TestBreakfastOnlyFromQuickAndStaple(t *testing.T) {
	res := generate(t, Request{MealSlots: defaultMealSlots(), Dishes: bigLibrary()})

	for _, it := range res.Items {
		if it.Slot != database.SlotBreakfast {
			continue
		}
		d := findDish(bigLibrary(), it.DishID)
		if d == nil {
			t.Fatalf("选出了不存在的菜: %d", it.DishID)
		}
		if d.Kind != database.KindStap && d.Kind != database.KindQuick {
			t.Fatalf("早餐出现了非快手/主食的菜: %s(%s)", d.Name, d.Kind)
		}
	}
}

func TestMeatMainIngredientNotRepeatedWithin48Hours(t *testing.T) {
	library := bigLibrary()
	res := generate(t, Request{MealSlots: defaultMealSlots(), Dishes: library})

	lastDay := map[string]int{}
	for _, it := range res.Items {
		d := findDish(library, it.DishID)
		if d == nil || d.Kind != database.KindMeat {
			continue
		}
		if last, ok := lastDay[d.MainIngredient]; ok && abs(last-it.DayIndex) < 2 {
			t.Fatalf("%s 在 %d 天和 %d 天连续出现（主料 %s）", d.Name, last, it.DayIndex, d.MainIngredient)
		}
		lastDay[d.MainIngredient] = it.DayIndex
	}
}

func TestBlockedAndAvoidedDishesNeverAppear(t *testing.T) {
	library := bigLibrary()
	blockedID := library[0].ID
	library[0].Blocked = true
	library[1].Tags = []string{"花生"}
	avoidID := library[1].ID

	res := generate(t, Request{MealSlots: defaultMealSlots(), Dishes: library, AvoidTags: []string{"花生"}})
	for _, it := range res.Items {
		if it.DishID == blockedID {
			t.Fatalf("拉黑的菜 %s 仍然出现在菜单里", library[0].Name)
		}
		if it.DishID == avoidID {
			t.Fatalf("忌口的菜 %s 仍然出现在菜单里", library[1].Name)
		}
	}
}

func TestEveryMainMealDayHasGreenVegetable(t *testing.T) {
	library := bigLibrary()
	res := generate(t, Request{MealSlots: defaultMealSlots(), Dishes: library})

	green := map[int]bool{}
	for _, it := range res.Items {
		if d := findDish(library, it.DishID); d != nil && d.Green {
			green[it.DayIndex] = true
		}
	}
	for day := 0; day < 7; day++ {
		if !green[day] {
			t.Fatalf("第 %d 天没有绿叶菜", day)
		}
	}
}

// 菜库极小的时候必须能降级出结果，而不是卡在互相矛盾的约束里
func TestTinyLibraryStillProducesResult(t *testing.T) {
	library := []database.Dish{
		makeDish(1, "唯一的荤菜", "鸡", database.KindMeat, []string{database.SlotDinner}, 30),
		makeDish(2, "唯一的素菜", "素", database.KindVeg, []string{database.SlotDinner}, 20),
		makeDish(3, "唯一的汤", "素", database.KindSoup, []string{database.SlotDinner}, 60),
		makeDish(4, "唯一的早餐", "素", database.KindStap, []string{database.SlotBreakfast}, 10),
		makeDish(5, "唯一的蛋白", "蛋", database.KindQuick, []string{database.SlotBreakfast}, 10),
	}

	done := make(chan *Result, 1)
	go func() {
		done <- generate(t, Request{MealSlots: defaultMealSlots(), Dishes: library})
	}()

	select {
	case res := <-done:
		if len(res.Items) == 0 {
			t.Fatal("菜库极小也应给出结果")
		}
		if res.Relaxed == 0 {
			t.Fatal("菜库不足时应明确标记已放宽约束")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("生成超时，疑似死循环")
	}
}

func TestLockedCellsAreKept(t *testing.T) {
	library := bigLibrary()
	lockedCell := Cell{DayIndex: 0, Slot: database.SlotDinner, Seq: 0}
	lockedDish := library[3].ID

	res := generate(t, Request{
		MealSlots: defaultMealSlots(),
		Dishes:    library,
		Existing:  map[Cell]int64{lockedCell: lockedDish},
		Locked:    map[Cell]bool{lockedCell: true},
	})

	for _, it := range res.Items {
		if it.Cell == lockedCell && it.DishID != lockedDish {
			t.Fatalf("锁定的格子被换掉了: %d -> %d", lockedDish, it.DishID)
		}
	}
}

func TestRerollSingleCellOnlyChangesThatCell(t *testing.T) {
	library := bigLibrary()
	existing := map[Cell]int64{}
	// 先排一整周
	first := generate(t, Request{MealSlots: defaultMealSlots(), Dishes: library})
	for _, it := range first.Items {
		existing[it.Cell] = it.DishID
	}

	target := Cell{DayIndex: 2, Slot: database.SlotDinner, Seq: 0}
	before := existing[target]

	targetCopy := target
	res := generate(t, Request{
		MealSlots: defaultMealSlots(),
		Dishes:    library,
		Existing:  existing,
		Target:    &targetCopy,
	})

	changed := false
	for _, it := range res.Items {
		if it.Cell == target {
			if it.DishID == before {
				t.Fatalf("换菜后仍然是同一道菜: %d", before)
			}
			changed = true
			continue
		}
		if it.DishID != existing[it.Cell] {
			t.Fatalf("换一格不该动其他格子: %v", it.Cell)
		}
	}
	if !changed {
		t.Fatal("目标格没有被替换")
	}
}

func TestEmptyLibraryReturnsError(t *testing.T) {
	g := NewLocalWeighted(rand.New(rand.NewSource(1)))
	if _, err := g.Generate(context.Background(), Request{MealSlots: defaultMealSlots()}); err != ErrNoDishes {
		t.Fatalf("空菜库应返回 ErrNoDishes，实际: %v", err)
	}
}

// TestKeepLockedRerollHasNoDuplicateCells 覆盖「保留锁定重排」的真实入参：
// existing 含整周所有格子，locked 只是其中一部分，且没有 Target。
// 修复前未锁定格会被计入两次，保存时撞 menu_items 唯一约束。
func TestKeepLockedRerollHasNoDuplicateCells(t *testing.T) {
	library := bigLibrary()
	first := generate(t, Request{MealSlots: defaultMealSlots(), Dishes: library})
	existing := map[Cell]int64{}
	for _, it := range first.Items {
		existing[it.Cell] = it.DishID
	}

	lockedCell := first.Items[0].Cell
	res := generate(t, Request{
		MealSlots: defaultMealSlots(),
		Dishes:    library,
		Existing:  existing,
		Locked:    map[Cell]bool{lockedCell: true},
	})

	seen := map[Cell]bool{}
	for _, it := range res.Items {
		if seen[it.Cell] {
			t.Fatalf("格子在结果里重复: %v", it.Cell)
		}
		seen[it.Cell] = true
		if it.Cell == lockedCell && it.DishID != existing[lockedCell] {
			t.Fatalf("锁定的格子被换掉了: %v", it.Cell)
		}
	}
	if len(res.Items) != len(first.Items) {
		t.Fatalf("格子数不对: 期望 %d, 实际 %d", len(first.Items), len(res.Items))
	}
}
