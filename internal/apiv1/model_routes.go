package apiv1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// RegisterModelRouteRoutes 挂载模型路由管理。
func RegisterModelRouteRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", listModelRoutes(a))
	g.POST("", createModelRoute(a))
	g.PUT("/:id", updateModelRoute(a))
	g.DELETE("/:id", deleteModelRoute(a))
}

type routeJSON struct {
	ID               int64  `json:"id"`
	DisplayName      string `json:"display_name"`
	ChannelID        int64  `json:"channel_id"`
	ChannelName      string `json:"channel_name"`
	ChannelEnabled   int    `json:"channel_enabled"`   // 所属渠道是否启用
	EffectiveEnabled int    `json:"effective_enabled"` // 有效 = 路由启用 AND 渠道启用
	UpstreamModel    string `json:"upstream_model"`
	Priority         int    `json:"priority"`
	Weight           int    `json:"weight"`
	Enabled          int    `json:"enabled"`
}

func toRouteJSON(r *models.ModelRoute, chName string, chEnabled int) routeJSON {
	eff := 0
	if r.Enabled == 1 && chEnabled == 1 {
		eff = 1
	}
	return routeJSON{
		ID: r.ID, DisplayName: r.DisplayName, ChannelID: r.ChannelID, ChannelName: chName,
		ChannelEnabled: chEnabled, EffectiveEnabled: eff,
		UpstreamModel: r.UpstreamModel, Priority: r.Priority, Weight: r.Weight, Enabled: r.Enabled,
	}
}

func listModelRoutes(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		routes, err := store.ListModelRoutes(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		channels, err := store.ListChannels(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		nameByID := map[int64]string{}
		enabledByID := map[int64]int{}
		for _, ch := range channels {
			nameByID[ch.ID] = ch.Name
			enabledByID[ch.ID] = ch.Enabled
		}
		out := make([]routeJSON, 0, len(routes))
		for _, r := range routes {
			out = append(out, toRouteJSON(r, nameByID[r.ChannelID], enabledByID[r.ChannelID]))
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func createModelRoute(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b struct {
			DisplayName   string `json:"display_name"`
			ChannelID     int64  `json:"channel_id"`
			UpstreamModel string `json:"upstream_model"`
			Priority      int    `json:"priority"`
			Weight        int    `json:"weight"`
			Enabled       *int   `json:"enabled"`
		}
		if !decode(c, &b) {
			return
		}
		if b.DisplayName == "" || b.ChannelID <= 0 || b.UpstreamModel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "display_name / channel_id / upstream_model 必填"})
			return
		}
		enabled := 1
		if b.Enabled != nil {
			enabled = *b.Enabled
		}
		weight := b.Weight
		if weight <= 0 {
			weight = 1
		}
		r := &models.ModelRoute{
			DisplayName: b.DisplayName, ChannelID: b.ChannelID, UpstreamModel: b.UpstreamModel,
			Priority: b.Priority, Weight: weight, Enabled: enabled,
		}
		id, err := store.CreateModelRoute(a.DB, r)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
	}
}

func updateModelRoute(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		var b struct {
			DisplayName   string `json:"display_name"`
			ChannelID     int64  `json:"channel_id"`
			UpstreamModel string `json:"upstream_model"`
			Priority      int    `json:"priority"`
			Weight        int    `json:"weight"`
			Enabled       *int   `json:"enabled"`
		}
		if !decode(c, &b) {
			return
		}
		b.Weight = normalizeWeight(b.Weight)
		cur, err := store.GetModelRoute(a.DB, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cur == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
			return
		}
		if b.DisplayName != "" {
			cur.DisplayName = b.DisplayName
		}
		if b.ChannelID > 0 {
			cur.ChannelID = b.ChannelID
		}
		if b.UpstreamModel != "" {
			cur.UpstreamModel = b.UpstreamModel
		}
		cur.Priority = b.Priority
		cur.Weight = b.Weight
		if b.Enabled != nil {
			cur.Enabled = *b.Enabled
		}
		if err := store.UpdateModelRoute(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 回显时补渠道启用状态（用于计算 effective_enabled）
		chEnabled := 0
		if ch, err := store.GetChannel(a.DB, cur.ChannelID); err == nil && ch != nil {
			chEnabled = ch.Enabled
		}
		c.JSON(http.StatusOK, gin.H{"data": toRouteJSON(cur, "", chEnabled)})
	}
}

func deleteModelRoute(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if err := store.DeleteModelRoute(a.DB, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

func normalizeWeight(w int) int {
	if w <= 0 {
		return 1
	}
	return w
}
