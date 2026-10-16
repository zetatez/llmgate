package apiv1

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// RegisterUserResourceRoutes 挂载用户端资源：渠道（套餐）自建、路由（只读+自管）、共享统计。
// 隔离规则：
//   - 所有人可读取全部渠道与路由；
//   - 仅能创建/修改/删除"自己拥有（user_id=本人）"的渠道及其下的路由；
//   - 共享统计（每人用量，含费用）对所有登录用户可见。
func RegisterUserResourceRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("/channels", userListChannels(a))
	g.POST("/channels", userCreateChannel(a))
	g.PUT("/channels/:id", userUpdateChannel(a))
	g.DELETE("/channels/:id", userDeleteChannel(a))

	g.GET("/routes", userListRoutes(a))
	g.POST("/routes", userCreateRoute(a))
	g.PUT("/routes/:id", userUpdateRoute(a))
	g.DELETE("/routes/:id", userDeleteRoute(a))

	g.GET("/usage/by-user", userUsageByUser(a))
}

// ---------------- 渠道（套餐） ----------------

type userChannelView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Adapter     string `json:"adapter"`
	BaseURL     string `json:"base_url"`
	Priority    int    `json:"priority"`
	Weight      int    `json:"weight"`
	Enabled     int    `json:"enabled"`
	HealthState string `json:"health_state"`
	Note        string `json:"note"`
	UserID      int64  `json:"user_id"`
	OwnerName   string `json:"owner_name"`
	IsMine      bool   `json:"is_mine"`
	MyKeys      int    `json:"my_keys"`
}

func myKeyCounts(a *app.App, userID int64) map[int64]int {
	m := map[int64]int{}
	rows, err := a.DB.Query(`SELECT channel_id, COUNT(*) FROM channel_keys WHERE user_id=? GROUP BY channel_id`, userID)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err == nil {
			m[id] = n
		}
	}
	return m
}

// userListChannels 返回全部渠道（所有人可见），并标注是否本人拥有、本人已录入的 Key 数。
func userListChannels(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		list, err := store.ListChannels(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		owners := userNames(a)
		counts := myKeyCounts(a, u.ID)
		out := make([]userChannelView, 0, len(list))
		for _, ch := range list {
			out = append(out, userChannelView{
				ID: ch.ID, Name: ch.Name, Adapter: ch.Adapter, BaseURL: ch.BaseURL,
				Priority: ch.Priority, Weight: ch.Weight, Enabled: ch.Enabled, HealthState: ch.HealthState, Note: ch.Note,
				UserID: ch.UserID, OwnerName: owners[ch.UserID], IsMine: ch.UserID == u.ID, MyKeys: counts[ch.ID],
			})
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func userCreateChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		var b struct {
			Name     string `json:"name"`
			BaseURL  string `json:"base_url"`
			Adapter  string `json:"adapter"`
			Priority int    `json:"priority"`
			Weight   int    `json:"weight"`
			Note     string `json:"note"`
		}
		if !decode(c, &b) {
			return
		}
		if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.BaseURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name and base_url are required"})
			return
		}
		adapter := strings.TrimSpace(b.Adapter)
		if adapter == "" {
			adapter = "openai"
		}
		ch := &models.Channel{
			UserID: u.ID, Name: strings.TrimSpace(b.Name), BaseURL: strings.TrimSpace(b.BaseURL),
			Adapter: adapter, Priority: b.Priority, Weight: orInt(b.Weight, 1), TimeoutMS: 60000,
			Enabled: 1, HealthState: "healthy", Note: b.Note,
		}
		id, err := store.CreateChannel(a.DB, ch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
	}
}

// ownedChannel 取当前用户拥有的渠道；不存在或无权限返回 nil（已写响应）。
func ownedChannel(a *app.App, c *gin.Context, id int64) *models.Channel {
	u := currentUser(c)
	ch, err := store.GetChannel(a.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil
	}
	if ch == nil || ch.UserID != u.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return nil
	}
	return ch
}

func userUpdateChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		cur := ownedChannel(a, c, id)
		if cur == nil {
			return
		}
		var b struct {
			Name     *string `json:"name"`
			BaseURL  *string `json:"base_url"`
			Adapter  *string `json:"adapter"`
			Priority *int    `json:"priority"`
			Weight   *int    `json:"weight"`
			Enabled  *int    `json:"enabled"`
			Note     *string `json:"note"`
		}
		if !decode(c, &b) {
			return
		}
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			cur.Name = strings.TrimSpace(*b.Name)
		}
		if b.BaseURL != nil && strings.TrimSpace(*b.BaseURL) != "" {
			cur.BaseURL = strings.TrimSpace(*b.BaseURL)
		}
		if b.Adapter != nil && strings.TrimSpace(*b.Adapter) != "" {
			cur.Adapter = strings.TrimSpace(*b.Adapter)
		}
		if b.Priority != nil {
			cur.Priority = *b.Priority
		}
		if b.Weight != nil && *b.Weight > 0 {
			cur.Weight = *b.Weight
		}
		if b.Enabled != nil {
			cur.Enabled = *b.Enabled
		}
		if b.Note != nil {
			cur.Note = *b.Note
		}
		if err := store.UpdateChannel(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": cur.ID}})
	}
}

func userDeleteChannel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if ownedChannel(a, c, id) == nil {
			return
		}
		if err := store.DeleteChannel(a.DB, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

// ---------------- 路由（所有人可见；仅本人渠道可改） ----------------

type userRouteView struct {
	ID               int64  `json:"id"`
	DisplayName      string `json:"display_name"`
	ChannelID        int64  `json:"channel_id"`
	ChannelName      string `json:"channel_name"`
	ChannelUserID    int64  `json:"channel_user_id"`
	UpstreamModel    string `json:"upstream_model"`
	Priority         int    `json:"priority"`
	Weight           int    `json:"weight"`
	Enabled          int    `json:"enabled"`
	EffectiveEnabled int    `json:"effective_enabled"`
	IsMine           bool   `json:"is_mine"`
}

func channelMap(a *app.App) (map[int64]*models.Channel, error) {
	list, err := store.ListChannels(a.DB)
	if err != nil {
		return nil, err
	}
	m := map[int64]*models.Channel{}
	for _, ch := range list {
		m[ch.ID] = ch
	}
	return m, nil
}

func userListRoutes(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		routes, err := store.ListModelRoutes(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		channels, err := channelMap(a)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out := make([]userRouteView, 0, len(routes))
		for _, r := range routes {
			ch := channels[r.ChannelID]
			v := userRouteView{
				ID: r.ID, DisplayName: r.DisplayName, ChannelID: r.ChannelID,
				UpstreamModel: r.UpstreamModel, Priority: r.Priority, Weight: r.Weight, Enabled: r.Enabled,
			}
			if ch != nil {
				v.ChannelName = ch.Name
				v.ChannelUserID = ch.UserID
				v.IsMine = ch.UserID == u.ID
				if ch.Enabled == 1 && r.Enabled == 1 {
					v.EffectiveEnabled = 1
				}
			}
			out = append(out, v)
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

// ownedRouteChannel 校验路由所属渠道由当前用户拥有。
func ownedRouteChannel(a *app.App, c *gin.Context, routeID int64) (*models.ModelRoute, bool) {
	r, err := store.GetModelRoute(a.DB, routeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	if r == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
		return nil, false
	}
	u := currentUser(c)
	ch, err := store.GetChannel(a.DB, r.ChannelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	if ch == nil || ch.UserID != u.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "route not found"})
		return nil, false
	}
	return r, true
}

func userCreateRoute(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
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
		if strings.TrimSpace(b.DisplayName) == "" || strings.TrimSpace(b.UpstreamModel) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "display_name and upstream_model are required"})
			return
		}
		ch, err := store.GetChannel(a.DB, b.ChannelID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if ch == nil || ch.UserID != u.ID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "you can only add routes to your own channels"})
			return
		}
		enabled := 1
		if b.Enabled != nil {
			enabled = *b.Enabled
		}
		r := &models.ModelRoute{
			DisplayName: strings.TrimSpace(b.DisplayName), ChannelID: b.ChannelID,
			UpstreamModel: strings.TrimSpace(b.UpstreamModel), Priority: b.Priority, Weight: orInt(b.Weight, 1), Enabled: enabled,
		}
		id, err := store.CreateModelRoute(a.DB, r)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"id": id}})
	}
}

func userUpdateRoute(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		cur, ok := ownedRouteChannel(a, c, id)
		if !ok {
			return
		}
		var b struct {
			DisplayName   *string `json:"display_name"`
			UpstreamModel *string `json:"upstream_model"`
			Priority      *int    `json:"priority"`
			Weight        *int    `json:"weight"`
			Enabled       *int    `json:"enabled"`
		}
		if !decode(c, &b) {
			return
		}
		if b.DisplayName != nil && strings.TrimSpace(*b.DisplayName) != "" {
			cur.DisplayName = strings.TrimSpace(*b.DisplayName)
		}
		if b.UpstreamModel != nil && strings.TrimSpace(*b.UpstreamModel) != "" {
			cur.UpstreamModel = strings.TrimSpace(*b.UpstreamModel)
		}
		if b.Priority != nil {
			cur.Priority = *b.Priority
		}
		if b.Weight != nil && *b.Weight > 0 {
			cur.Weight = *b.Weight
		}
		if b.Enabled != nil {
			cur.Enabled = *b.Enabled
		}
		if err := store.UpdateModelRoute(a.DB, cur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "ok"})
	}
}

func userDeleteRoute(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c, "id")
		if !ok {
			return
		}
		if _, ok := ownedRouteChannel(a, c, id); !ok {
			return
		}
		if err := store.DeleteModelRoute(a.DB, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": "deleted"})
	}
}

// ---------------- 共享统计（所有人可见，含费用） ----------------

// userUsageByUser 返回按用户聚合的用量（请求/成功/上传/下载/合计/费用），所有登录用户可见。
func userUsageByUser(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		days := 7
		if v := c.Query("days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 365 {
				days = n
			}
		}
		var from int64
		if days > 0 {
			from = dayStartOffset(days - 1)
		}
		list, err := store.UsageByUser(a.DB, from, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	}
}

func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
