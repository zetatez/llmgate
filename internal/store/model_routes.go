package store

import (
	"database/sql"
	"fmt"
	"strings"

	"llmgate/internal/models"
)

const routeCols = `id, display_name, channel_id, upstream_model, priority, weight, enabled, created_at, updated_at`

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
		if err := rows.Scan(&r.ID, &r.DisplayName, &r.ChannelID, &r.UpstreamModel, &r.Priority, &r.Weight, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
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
		if err := rows.Scan(&r.ID, &r.DisplayName, &r.ChannelID, &r.UpstreamModel, &r.Priority, &r.Weight, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateModelRoute 插入路由。
func CreateModelRoute(db *sql.DB, r *models.ModelRoute) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO model_routes (display_name, channel_id, upstream_model, priority, weight, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.DisplayName, r.ChannelID, r.UpstreamModel, r.Priority, r.Weight, r.Enabled, models.Now(), models.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateModelRoute 更新路由。
func UpdateModelRoute(db *sql.DB, r *models.ModelRoute) error {
	res, err := db.Exec(`
		UPDATE model_routes SET display_name=?, channel_id=?, upstream_model=?, priority=?, weight=?, enabled=?, updated_at=? WHERE id=?`,
		r.DisplayName, r.ChannelID, r.UpstreamModel, r.Priority, r.Weight, r.Enabled, models.Now(), r.ID)
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
	err := row.Scan(&r.ID, &r.DisplayName, &r.ChannelID, &r.UpstreamModel, &r.Priority, &r.Weight, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// RouteExists 判断「渠道 + 显示名 + 上游模型」的完整路由是否已存在（自动同步去重用）。
func RouteExists(db *sql.DB, channelID int64, displayName, upstreamModel string) (bool, error) {
	var n int64
	err := db.QueryRow(`SELECT COUNT(1) FROM model_routes WHERE channel_id=? AND display_name=? AND upstream_model=?`,
		channelID, displayName, upstreamModel).Scan(&n)
	return n > 0, err
}

// RemoveStaleRoutes 删除该渠道下"同名直通"(display_name=upstream_model) 但上游已不存在的路由。
// 手动别名映射（display_name != upstream_model）保留，避免误删用户自定义路由。
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
		WHERE channel_id = ? AND display_name = upstream_model
		  AND upstream_model NOT IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
