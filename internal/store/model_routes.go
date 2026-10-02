package store

import (
	"database/sql"
	"fmt"
	"strings"

	"llmgate/internal/models"
)

const routeCols = `id, display_name, channel_id, upstream_model, priority, weight, enabled, price_input, price_output, created_at, updated_at`

// ListModelRoutes 返回全部模型路由。
func ListModelRoutes(db *sql.DB) ([]*models.ModelRoute, error) {
	rows, err := db.Query(`SELECT ` + routeCols + ` FROM model_routes ORDER BY display_name ASC, priority ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ModelRoute
	for rows.Next() {
		r := &models.ModelRoute{}
		if err := rows.Scan(&r.ID, &r.DisplayName, &r.ChannelID, &r.UpstreamModel, &r.Priority, &r.Weight, &r.Enabled, &r.PriceInput, &r.PriceOutput, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListRoutesForModel 返回某对外模型名的全部可用路由（es. 用于网关选择）。
func ListRoutesForModel(db *sql.DB, displayName string) ([]*models.ModelRoute, error) {
	rows, err := db.Query(`SELECT `+routeCols+` FROM model_routes WHERE display_name=? ORDER BY priority ASC, id ASC`, displayName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ModelRoute
	for rows.Next() {
		r := &models.ModelRoute{}
		if err := rows.Scan(&r.ID, &r.DisplayName, &r.ChannelID, &r.UpstreamModel, &r.Priority, &r.Weight, &r.Enabled, &r.PriceInput, &r.PriceOutput, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateModelRoute 插入路由。
func CreateModelRoute(db *sql.DB, r *models.ModelRoute) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO model_routes (display_name, channel_id, upstream_model, priority, weight, enabled, price_input, price_output, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.DisplayName, r.ChannelID, r.UpstreamModel, r.Priority, r.Weight, r.Enabled,
		r.PriceInput, r.PriceOutput, models.Now(), models.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateModelRoute 更新路由。
func UpdateModelRoute(db *sql.DB, r *models.ModelRoute) error {
	res, err := db.Exec(`
		UPDATE model_routes SET display_name=?, channel_id=?, upstream_model=?, priority=?, weight=?, enabled=?, price_input=?, price_output=?, updated_at=? WHERE id=?`,
		r.DisplayName, r.ChannelID, r.UpstreamModel, r.Priority, r.Weight, r.Enabled,
		r.PriceInput, r.PriceOutput, models.Now(), r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("route %d not found", r.ID)
	}
	return nil
}

// DeleteModelRoute 删除路由。
func DeleteModelRoute(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM model_routes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("route %d not found", id)
	}
	return nil
}

// GetModelRoute 按 id 获取路由。
func GetModelRoute(db *sql.DB, id int64) (*models.ModelRoute, error) {
	row := db.QueryRow(`SELECT `+routeCols+` FROM model_routes WHERE id=?`, id)
	r := &models.ModelRoute{}
	err := row.Scan(&r.ID, &r.DisplayName, &r.ChannelID, &r.UpstreamModel, &r.Priority, &r.Weight, &r.Enabled, &r.PriceInput, &r.PriceOutput, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// DeleteAutoBare 删除该渠道的"纯裸名自动路由"（display_name=upstream_model 且不含 '/'），
// 用于收敛到厂商前缀命名。手动别名（display 与 upstream 不同）不受影响。
func DeleteAutoBare(db *sql.DB, channelID int64, upstreamModel string) (int64, error) {
	res, err := db.Exec(`
		DELETE FROM model_routes
		WHERE channel_id = ? AND upstream_model = ?
		  AND display_name = upstream_model AND display_name NOT LIKE '%/%'`,
		channelID, upstreamModel)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ProbeRouteModel 返回渠道首个启用路由的上游模型名（探活用；无则空串）。
func ProbeRouteModel(db *sql.DB, channelID int64) (string, error) {
	var m string
	err := db.QueryRow(
		`SELECT upstream_model FROM model_routes WHERE channel_id=? AND enabled=1 ORDER BY priority ASC, id ASC LIMIT 1`,
		channelID).Scan(&m)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return m, err
}

// RouteExists 判断「渠道 + 显示名 + 上游模型」的完整路由是否已存在（自动同步去重用）。
func RouteExists(db *sql.DB, channelID int64, displayName, upstreamModel string) (bool, error) {
	var n int64
	err := db.QueryRow(`SELECT COUNT(1) FROM model_routes WHERE channel_id=? AND display_name=? AND upstream_model=?`,
		channelID, displayName, upstreamModel).Scan(&n)
	return n > 0, err
}

// RemoveStaleRoutes 删除该渠道下已失效的自动路由：上游已不存在的模型。
// 自动路由包括"同名直通"(display=upstream) 与"厂商前缀别名"(display=vendor/upstream)；
// 手动别名映射（display 与 upstream 无关）保留，避免误删用户自定义路由。
func RemoveStaleRoutes(db *sql.DB, channelID int64, validModels []string) (int64, error) {
	if len(validModels) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(validModels)), ",")
	args := make([]any, 0, len(validModels)+1)
	args = append(args, channelID)
	for _, m := range validModels {
		args = append(args, m)
	}
	res, err := db.Exec(`
		DELETE FROM model_routes
		WHERE channel_id = ?
		  AND upstream_model NOT IN (`+placeholders+`)
		  AND (display_name = upstream_model
		       OR (INSTR(display_name, '/') > 0
		           AND SUBSTR(display_name, INSTR(display_name, '/') + 1) = upstream_model))`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
