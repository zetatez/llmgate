package store

import (
	"database/sql"
	"fmt"
	"strings"

	"llmgate/internal/models"
)

// ListModelPrices 返回全部模型价格（按模型名排序）。
func ListModelPrices(db *sql.DB) ([]models.ModelPrice, error) {
	rows, err := db.Query(`SELECT model, input, output, call, updated_at FROM model_prices ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ModelPrice, 0)
	for rows.Next() {
		var p models.ModelPrice
		if err := rows.Scan(&p.Model, &p.Input, &p.Output, &p.Call, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountModelPrices 返回价格表记录数（用于从旧 settings 导入时判断是否为空）。
func CountModelPrices(db *sql.DB) (int64, error) {
	var n int64
	err := db.QueryRow(`SELECT COUNT(1) FROM model_prices`).Scan(&n)
	return n, err
}

// UpsertModelPrices 批量新增/更新模型价格（单事务，updated_at 统一）。
func UpsertModelPrices(db *sql.DB, prices []models.ModelPrice) error {
	if len(prices) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := models.Now()
	stmt, err := tx.Prepare(`
		INSERT INTO model_prices (model, input, output, call, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(model) DO UPDATE SET
			input = excluded.input,
			output = excluded.output,
			call = excluded.call,
			updated_at = excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range prices {
		model := strings.TrimSpace(p.Model)
		if model == "" {
			return fmt.Errorf("model is required")
		}
		if _, err := stmt.Exec(model, p.Input, p.Output, p.Call, now); err != nil {
			return fmt.Errorf("upsert %s: %w", model, err)
		}
	}
	return tx.Commit()
}

// DeleteModelPrices 批量删除模型价格。
func DeleteModelPrices(db *sql.DB, modelNames []string) error {
	if len(modelNames) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`DELETE FROM model_prices WHERE model = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range modelNames {
		if _, err := stmt.Exec(strings.TrimSpace(m)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
