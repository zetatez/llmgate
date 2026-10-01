package store

import (
	"database/sql"
	"fmt"

	"llmgate/internal/models"
)

// ---------------- 渠道（Channel） ----------------

const channelCols = `id, name, base_url, adapter, priority, weight, timeout_ms, enabled, health_state, cooldown_until, note, created_at, updated_at`

func scanChannel(row interface{ Scan(...any) error }) (*models.Channel, error) {
	c := &models.Channel{}
	err := row.Scan(&c.ID, &c.Name, &c.BaseURL, &c.Adapter, &c.Priority, &c.Weight,
		&c.TimeoutMS, &c.Enabled, &c.HealthState, &c.CooldownUntil, &c.Note, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ListChannels 返回全部渠道（优先级升序，同优先级按权重降序——权重大的排前面）。
func ListChannels(db *sql.DB) ([]*models.Channel, error) {
	rows, err := db.Query(`SELECT ` + channelCols + ` FROM channels ORDER BY priority ASC, weight DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChannel 按 id 获取渠道。
func GetChannel(db *sql.DB, id int64) (*models.Channel, error) {
	row := db.QueryRow(`SELECT `+channelCols+` FROM channels WHERE id = ?`, id)
	c, err := scanChannel(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// CreateChannel 插入渠道并返回新 id。
func CreateChannel(db *sql.DB, c *models.Channel) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO channels (name, base_url, adapter, priority, weight, timeout_ms,
			enabled, health_state, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.BaseURL, c.Adapter, c.Priority, c.Weight, c.TimeoutMS,
		c.Enabled, c.HealthState, c.Note, models.Now(), models.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateChannel 更新渠道字段（name/base_url/priority/weight/timeout/enabled/note）。
func UpdateChannel(db *sql.DB, c *models.Channel) error {
	res, err := db.Exec(`
		UPDATE channels SET name=?, base_url=?, adapter=?, priority=?, weight=?,
			timeout_ms=?, enabled=?, note=?, updated_at=? WHERE id=?`,
		c.Name, c.BaseURL, c.Adapter, c.Priority, c.Weight, c.TimeoutMS,
		c.Enabled, c.Note, models.Now(), c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %d not found", c.ID)
	}
	return nil
}

// DeleteChannel 删除渠道（channel_keys/model_routes 级联删除）。
func DeleteChannel(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %d not found", id)
	}
	return nil
}

// SetChannelCooldown 将渠道置为冷却并写入截止时间（熔断持久化）。
func SetChannelCooldown(db *sql.DB, id int64, until int64) error {
	_, err := db.Exec(`UPDATE channels SET health_state='cooldown', cooldown_until=?, updated_at=? WHERE id=?`, until, models.Now(), id)
	return err
}

// SetChannelHealthy 恢复渠道健康（清冷却标记）。
func SetChannelHealthy(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE channels SET health_state='healthy', cooldown_until=0, updated_at=? WHERE id=?`, models.Now(), id)
	return err
}

// ListCooldownChannels 返回冷却中且已到期的渠道（供探活任务尝试恢复）。
func ListCooldownChannels(db *sql.DB, dueAt int64) ([]*models.Channel, error) {
	rows, err := db.Query(`SELECT `+channelCols+` FROM channels WHERE health_state='cooldown' AND cooldown_until <= ?`, dueAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------- 渠道 Key 池 ----------------

const keyCols = `id, channel_id, name, source, remark, api_key_enc, enabled, weight, last_used_at, created_at`

// KeyInput 新增 Key 时的输入（调用方已加密）。
type KeyInput struct {
	Name      string
	Source    string
	Remark    string
	Encrypted string
}

func scanKey(row interface{ Scan(...any) error }) (*models.ChannelKey, error) {
	k := &models.ChannelKey{}
	err := row.Scan(&k.ID, &k.ChannelID, &k.Name, &k.Source, &k.Remark, &k.APIKeyEnc, &k.Enabled, &k.Weight, &k.LastUsedAt, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

// ListChannelKeys 返回渠道下全部 Key（含名称，不含明文）。
func ListChannelKeys(db *sql.DB, channelID int64) ([]*models.ChannelKey, error) {
	rows, err := db.Query(`SELECT `+keyCols+` FROM channel_keys WHERE channel_id=? ORDER BY id ASC`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.ChannelKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// AddChannelKeys 批量插入渠道 Key（调用方已加密，可带名称）。
func AddChannelKeys(db *sql.DB, channelID int64, items []KeyInput) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO channel_keys (channel_id, name, source, remark, api_key_enc, enabled, weight, created_at) VALUES (?, ?, ?, ?, ?, 1, 1, ?)`,
			channelID, it.Name, it.Source, it.Remark, it.Encrypted, models.Now()); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// GetChannelKey 获取单个 Key。
func GetChannelKey(db *sql.DB, id int64) (*models.ChannelKey, error) {
	row := db.QueryRow(`SELECT `+keyCols+` FROM channel_keys WHERE id=?`, id)
	k, err := scanKey(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// UpdateChannelKey 更新 Key 的名称、来源、备注、启用状态与权重。
func UpdateChannelKey(db *sql.DB, k *models.ChannelKey) error {
	res, err := db.Exec(`UPDATE channel_keys SET name=?, source=?, remark=?, enabled=?, weight=? WHERE id=?`,
		k.Name, k.Source, k.Remark, k.Enabled, k.Weight, k.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("key %d not found", k.ID)
	}
	return nil
}

// UpdateChannelKeySecret 更换 Key 的密文（调用方已加密）。
func UpdateChannelKeySecret(db *sql.DB, id int64, encrypted string) error {
	res, err := db.Exec(`UPDATE channel_keys SET api_key_enc=? WHERE id=?`, encrypted, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("key %d not found", id)
	}
	return nil
}

// DeleteChannelKey 删除某个 Key。
func DeleteChannelKey(db *sql.DB, id int64) error {
	res, err := db.Exec(`DELETE FROM channel_keys WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("key %d not found", id)
	}
	return nil
}

// TouchChannelKey 更新 Key 最近使用时间。
func TouchChannelKey(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE channel_keys SET last_used_at=? WHERE id=?`, models.Now(), id)
	return err
}
