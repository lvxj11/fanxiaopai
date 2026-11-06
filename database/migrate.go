package database

import (
	"database/sql"
	"fmt"
)

// 迁移框架：用 SQLite 的 PRAGMA user_version 记录结构版本。
//
// 开发期变动剧烈，迁移会被阶段性清理（参考之前删掉 migrate() 的那次）：
//   - 需要加列时：把迁移函数按序追加到 steps，schemaVersion 会自动 +1；
//   - 阶段性清理时：清空 steps、把 baseline 重置（user_version 语义见下），
//     并在 README「不兼容变更记录」里记一条。
//
// 版本基本稳定之前，说明里只保留不兼容记录，不写详细迁移文档。
//
// 版本语义：
//   - 0 = 从未打过版本（全新库，或旧开发库）
//   - 1 = baseline；steps[i] 负责把库从版本 i+1 升到 i+2
//   - schemaVersion = len(steps)+1，即当前程序期望的最高版本
//
// 全新库由 createTables() 用 IF NOT EXISTS 直接建成最新结构，所以 version=0 的库
// 不需要跑增量步骤，只要校验结构齐全即可「采纳」并直接盖到 schemaVersion。
// steps 只服务「已打过版本但落后」的库。
// steps 有序迁移步骤；steps[i] 把 user_version 从 i+1 升到 i+2。
// 当前处于 baseline，无增量迁移，留空。
var steps = []func(*sql.Tx) error{}

// schemaVersion 当前程序期望的最高 user_version。改动 steps 后自动 +1，无需手动同步。
var schemaVersion = len(steps) + 1

// migrate 校验并推进数据库结构。
// createTables() 对已存在的表不会补列，因此这里显式校验关键列，
// 拦下不兼容的旧库并给出明确重建提示，而不是让程序带着坏结构运行到某条 SQL 才崩。
func (d *Database) migrate() error {
	var current int
	if err := d.db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("读取数据库版本失败: %v", err)
	}

	if current > schemaVersion {
		return fmt.Errorf(
			"数据库版本(%d)高于当前程序(%d)：开发期清理过迁移，旧库不再兼容。请删除 data/menu.db 后重启，重新在 /setup 初始化",
			current, schemaVersion)
	}

	hasCore, err := d.dishesHaveRecipePrep()
	if err != nil {
		return err
	}
	if !hasCore {
		return fmt.Errorf(
			"数据库结构过旧：dishes 缺少 recipe/prep 列。本项目尚未正式对外发布，不提供旧库迁移，请删除 data/menu.db 后重启重新初始化")
	}

	if current == schemaVersion {
		return nil // 已是最新，重复启动幂等
	}

	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// current==0：全新库或从未打过版本的旧开发库，结构已由 createTables() 建齐并经上面校验，
	//             直接盖版本即可。
	// current>=1：已打过版本但落后，按序执行 steps[current-1:] 补齐结构。
	for i := current; i >= 1 && i-1 < len(steps); i++ {
		if err := steps[i-1](tx); err != nil {
			return fmt.Errorf("执行迁移步骤 v%d→v%d 失败: %v", i, i+1, err)
		}
	}

	// PRAGMA 不支持占位符参数，只能拼字符串；schemaVersion 是常量整数，无注入风险
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("写入数据库版本失败: %v", err)
	}
	return tx.Commit()
}

// dishesHaveRecipePrep 检查 dishes 是否具备 recipe/prep 列。
// 这两列是「简化迁移」之前那批旧库缺的，也是当前代码查询强依赖的列。
// 将来新增必需列时，在这里一并扩展校验项。
func (d *Database) dishesHaveRecipePrep() (bool, error) {
	rows, err := d.db.Query("PRAGMA table_info(dishes)")
	if err != nil {
		return false, fmt.Errorf("读取 dishes 表结构失败: %v", err)
	}
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return found["recipe"] && found["prep"], nil
}
