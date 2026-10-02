package apiv1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"llmgate/internal/adapter"
	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// RegisterChannelRoutes 挂载渠道及 Key 池管理路由。
func RegisterChannelRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", listChannels(a))
	g.POST("", createChannel(a))
	g.POST("/:id/test", testChannel(a))

	g.GET("/:id", getChannel(a))
	g.PUT("/:id", updateChannel(a))
	g.DELETE("/:id", deleteChannel(a))

	g.GET("/:id/keys", listKeys(a))
	g.POST("/:id/keys", addKeys(a))
	g.PUT("/keys/:keyID", updateKey(a))
	g.DELETE("/keys/:keyID", deleteKey(a))
}

type channelBody struct {
	Name         string            `json:"name"`
	BaseURL      string            `json:"base_url"`
	Adapter      string            `json:"adapter"`
	Priority     int               `json:"priority"`
	Weight       int               `json:"weight"`
	TimeoutMS    int               `json:"timeout_ms"`
	Enabled      *int              `json:"enabled"`
	Note         string            `json:"note"`
	ExtraHeaders map[string]string `json:"extra_headers"` // 渠道附加请求头，如 {"x-opencode-session":"..."}
	Keys         json.RawMessage   `json:"keys"`          // 创建时可同时提交：["sk-x"] 或 [{"name":"主号","key":"sk-x"}]
}

func (b *channelBody) toModel() *models.Channel {
	c := &models.Channel{
		Name:        b.Name,
		BaseURL:     b.BaseURL,
		Adapter:     b.Adapter,
		Priority:    b.Priority,
		Weight:      b.Weight,
		TimeoutMS:   b.TimeoutMS,
		Note:        b.Note,
		HealthState: "healthy",
	}
	if len(b.ExtraHeaders) > 0 {
		if hb, err := json.Marshal(b.ExtraHeaders); err == nil {
			c.ExtraHeaders = string(hb)
		}
	}
	if c.Adapter == "" {
		c.Adapter = "openai"
	}
	if c.Weight <= 0 {
		c.Weight = 1
	}
	if c.TimeoutMS <= 0 {
		c.TimeoutMS = 60000
	}
	c.Enabled = 1
	if b.Enabled != nil {
		c.Enabled = *b.Enabled
	}
	return c
}

func listChannels(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := store.ListChannels(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	}
}

func getChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		ch, err := store.GetChannel(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if ch == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": ch})
	}
}

func createChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b channelBody
		if !decode(c, &b) {
			return
		}
		if b.Name == "" || b.BaseURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name 与 base_url 必填"})
			return
		}
		ch := b.toModel()
		id, err := store.CreateChannel(a.DB, ch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 创建渠道时随 body 提交的 Key 一并加密保存（单次绑定，避免二次读流）
		if len(b.Keys) > 0 {
			entries, err := parseKeyEntries(b.Keys)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			for _, e := range entries {
				if e.Key == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "key 不能为空"})
					return
				}
			}
			if err := encryptAndAddKeyItems(a, id, entries); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
	}
}

// updateChannel 部分更新渠道：只更新请求体中出现的字段（其余保持不变），
// 避免"快捷启停"等只传单字段的调用清空其它配置。
func updateChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		var b struct {
			Name         *string           `json:"name"`
			BaseURL      *string           `json:"base_url"`
			Adapter      *string           `json:"adapter"`
			Priority     *int              `json:"priority"`
			Weight       *int              `json:"weight"`
			TimeoutMS    *int              `json:"timeout_ms"`
			Enabled      *int              `json:"enabled"`
			Note         *string           `json:"note"`
			ExtraHeaders map[string]string `json:"extra_headers"`
		}
		if !decode(c, &b) {
			return
		}
		cur, err := store.GetChannel(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cur == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		if b.Name != nil {
			cur.Name = *b.Name
		}
		if b.BaseURL != nil {
			cur.BaseURL = *b.BaseURL
		}
		if b.Adapter != nil && *b.Adapter != "" {
			cur.Adapter = *b.Adapter
		}
		if b.Priority != nil {
			cur.Priority = *b.Priority
		}
		if b.Weight != nil {
			cur.Weight = *b.Weight
			if cur.Weight <= 0 {
				cur.Weight = 1
			}
		}
		if b.TimeoutMS != nil {
			cur.TimeoutMS = *b.TimeoutMS
			if cur.TimeoutMS <= 0 {
				cur.TimeoutMS = 60000
			}
		}
		if b.Enabled != nil {
			cur.Enabled = *b.Enabled
		}
		if b.Note != nil {
			cur.Note = *b.Note
		}
		if b.ExtraHeaders != nil {
			if hb, err := json.Marshal(b.ExtraHeaders); err == nil {
				cur.ExtraHeaders = string(hb)
			}
		}
		if err := store.UpdateChannel(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": cur})
	}
}

func deleteChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if err := store.DeleteChannel(a.DB, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

// testChannel 用渠道第一个启用 Key 请求上游 /v1/models，验证连通性。
func testChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		ch, err := store.GetChannel(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if ch == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		apiKey, err := a.FirstEnabledKey(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		adpt := adapter.Get(ch.Adapter)
		if adpt == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未知 adapter 类型: " + ch.Adapter})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(ch.TimeoutMS)*time.Millisecond)
		defer cancel()

		start := time.Now()
		resp, err := adpt.Do(ctx, adapter.ChannelFrom(ch), apiKey, adapter.TestRequest())
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": false, "error": err.Error(), "latency_ms": time.Since(start).Milliseconds()}})
			return
		}
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": false, "status": resp.StatusCode, "error": string(body), "latency_ms": time.Since(start).Milliseconds()}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": true, "status": resp.StatusCode, "latency_ms": time.Since(start).Milliseconds()}})
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

var _ = truncateStr // 保留工具函数（后续错误摘要可能用到）

// ---- Key 池 ----

type keyView struct {
	ID               int64  `json:"id"`
	ChannelID        int64  `json:"channel_id"`
	Name             string `json:"name"`   // Key 名称（管理用）
	Source           string `json:"source"` // 来源
	Remark           string `json:"remark"` // 备注
	Masked           string `json:"masked"`
	KeyTail          string `json:"key_tail"`          // 明文末 4 位（辨认用）
	Enabled          int    `json:"enabled"`           // Key 自身状态（独立管理）
	EffectiveEnabled int    `json:"effective_enabled"` // 有效状态 = 渠道启用 && Key 启用
	Weight           int    `json:"weight"`
	LastUsedAt       int64  `json:"last_used_at"`
}

func toKeyView(a *app.App, k *models.ChannelKey) keyView {
	return keyView{ID: k.ID, ChannelID: k.ChannelID, Name: k.Name, Source: k.Source, Remark: k.Remark,
		Masked: "sk-****", KeyTail: a.KeyTail(k.APIKeyEnc), Enabled: k.Enabled, Weight: k.Weight, LastUsedAt: k.LastUsedAt}
}

func listKeys(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		// 渠道启用状态用于计算 Key 的"有效状态"（不修改 Key 存储值）
		channelEnabled := 0
		if ch, err := store.GetChannel(a.DB, id); err == nil && ch != nil {
			channelEnabled = ch.Enabled
		}
		keys, err := store.ListChannelKeys(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]keyView, 0, len(keys))
		for _, k := range keys {
			v := toKeyView(a, k)
			if channelEnabled == 1 && k.Enabled == 1 {
				v.EffectiveEnabled = 1
			}
			out = append(out, v)
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

// addKeysBody 兼容两种体：
//
//	{"keys": ["sk-a", "sk-b"]}                                             纯字符串
//	{"keys": [{"name": "主号", "source": "官网", "remark": "...", "key": "sk-a"}]}  带名称/来源/备注
type addKeysBody struct {
	Keys json.RawMessage `json:"keys"`
}

type keyEntry struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Remark string `json:"remark"`
	Key    string `json:"key"`
}

// parseKeyEntries 解析 keys 字段为 keyEntry 列表。
func parseKeyEntries(raw json.RawMessage) ([]keyEntry, error) {
	entries := []keyEntry{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return entries, nil
	}
	if trimmed[0] == '[' {
		var objs []keyEntry
		if err := json.Unmarshal(trimmed, &objs); err == nil {
			return objs, nil
		}
		var strs []string
		if err := json.Unmarshal(trimmed, &strs); err != nil {
			return nil, errors.New("keys 必须是字符串数组或 {name,source,remark,key} 对象数组")
		}
		for _, s := range strs {
			entries = append(entries, keyEntry{Key: s})
		}
		return entries, nil
	}
	return nil, errors.New("keys 必须是数组")
}

func addKeys(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		var b addKeysBody
		if !decode(c, &b) {
			return
		}
		entries, err := parseKeyEntries(b.Keys)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(entries) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "keys 不能为空"})
			return
		}
		for _, e := range entries {
			if e.Key == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "key 不能为空"})
				return
			}
		}
		if err := encryptAndAddKeyItems(a, id, entries); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"added": len(entries)}})
	}
}

func encryptAndAddKeyItems(a *app.App, channelID int64, entries []keyEntry) error {
	items := make([]store.KeyInput, 0, len(entries))
	for _, e := range entries {
		enc, err := a.Secret.Encrypt(e.Key)
		if err != nil {
			return err
		}
		items = append(items, store.KeyInput{Name: e.Name, Source: e.Source, Remark: e.Remark, Encrypted: enc})
	}
	return store.AddChannelKeys(a.DB, channelID, items)
}

func updateKey(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "keyID")
		if !ok {
			return
		}
		var b struct {
			Name    *string `json:"name"`
			Source  *string `json:"source"`
			Remark  *string `json:"remark"`
			Key     *string `json:"key"` // 提供即更换密钥
			Enabled *int    `json:"enabled"`
			Weight  *int    `json:"weight"`
		}
		if !decode(c, &b) {
			return
		}
		cur, err := store.GetChannelKey(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cur == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
			return
		}
		if b.Name != nil {
			cur.Name = *b.Name
		}
		if b.Source != nil {
			cur.Source = *b.Source
		}
		if b.Remark != nil {
			cur.Remark = *b.Remark
		}
		if b.Enabled != nil {
			cur.Enabled = *b.Enabled
		}
		if b.Weight != nil && *b.Weight > 0 {
			cur.Weight = *b.Weight
		}
		if err := store.UpdateChannelKey(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if b.Key != nil && strings.TrimSpace(*b.Key) != "" {
			enc, err := a.Secret.Encrypt(strings.TrimSpace(*b.Key))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if err := store.UpdateChannelKeySecret(a.DB, id, enc); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			cur.APIKeyEnc = enc
		}
		c.JSON(http.StatusOK, gin.H{"data": toKeyView(a, cur)})
	}
}

func deleteKey(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "keyID")
		if !ok {
			return
		}
		if err := store.DeleteChannelKey(a.DB, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}
