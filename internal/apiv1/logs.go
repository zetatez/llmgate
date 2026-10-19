package apiv1

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"llmgate/internal/app"
	"llmgate/internal/models"
	"llmgate/internal/store"
)

// RegisterLogRoutes 挂载日志与统计路由。
func RegisterLogRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", queryLogs(a))
	g.GET("/count", countLogs(a))
	g.GET("/usage/dashboard", usageDashboard(a))
	g.GET("/usage/by-model", usageByModel(a))
	g.GET("/usage/by-user-model", usageByUserModel(a))
}

// usageByUserModel 用户×模型用量。days=1 今天 / 7 近7天 / 0 全部（默认 7）。
func usageByUserModel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		days := 7
		if v := c.Query("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err == nil && n >= 0 {
				days = n
			}
		}
		var from, to int64
		if days > 0 {
			from = dayStartOffset(days - 1)
		}
		list, err := store.UsageByUserModel(a.DB, from, to)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	}
}

// dayStartOffset 返回 N 天前 0 点的 unix 秒（本地时区）。
func dayStartOffset(n int) int64 {
	now := time.Now()
	day := now.AddDate(0, 0, -n)
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()).Unix()
}

// usageByModel 按模型聚合某天用量占比。date=YYYY-MM-DD，缺省为今天。
func usageByModel(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		dateStr := c.Query("date")
		day := time.Now()
		if dateStr != "" {
			t, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "date must be in YYYY-MM-DD format"})
				return
			}
			day = t
		}
		from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local).Unix()
		to := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, time.Local).Unix()
		list, err := store.UsageByModel(a.DB, from, to)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"date": day.Format("2006-01-02"), "models": list}})
	}
}

func queryLogs(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := parseLogFilter(c)
		list, err := store.QueryLogs(a.DB, f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	}
}

// countLogs 返回满足筛选条件的日志总数（分页用）。
func countLogs(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		n, err := store.CountLogs(a.DB, parseLogFilter(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": n})
	}
}

// parseLogFilter 从查询参数构造日志筛选条件（管理端）。
func parseLogFilter(c *gin.Context) store.LogFilter {
	f := store.LogFilter{
		DisplayModel: c.Query("display_model"),
		Status:       c.Query("status"),
	}
	if v := c.Query("channel_id"); v != "" {
		id, _ := strconv.ParseInt(v, 10, 64)
		f.ChannelID = id
	}
	if v := c.Query("user_id"); v != "" {
		id, _ := strconv.ParseInt(v, 10, 64)
		f.UserID = id
	}
	if v := c.Query("from"); v != "" {
		f.From, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("to"); v != "" {
		f.To, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("offset"); v != "" {
		f.Offset, _ = strconv.Atoi(v)
	}
	if v := c.Query("limit"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}
	return f
}

// dashboardPayload 仪表盘一次性数据。
type dashboardPayload struct {
	Days           int                     `json:"days"`
	Week           []store.UsagePoint      `json:"week"`
	WeekModels     []store.UsageModelPoint `json:"week_models"`
	Models         []store.ModelUsage      `json:"models"`      // 区间内按模型聚合
	ModelsPrev     []store.ModelUsage      `json:"models_prev"` // 上一等长区间，用于环比
	Hours          []store.UsageHourPoint  `json:"hours"`
	Today          store.UsageSummary      `json:"today"`
	ChannelStats   []store.ChannelDayStats `json:"channel_stats"`
	Failures       []*models.RequestLog    `json:"failures"` // 最近失败明细
	RecentFailures int                     `json:"recent_failures"`
}

func usageDashboard(a *app.App) gin.HandlerFunc {
	return dashboardPayloadHandler(a, 0)
}

// userDashboard 普通用户仪表盘：与 admin 同结构，但数据仅限本人。
func userDashboard(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		dashboardPayloadHandler(a, u.ID)(c)
	}
}

// dashboardPayloadHandler 生成仪表盘数据；userID=0 表示全平台，>0 表示仅该用户。
func dashboardPayloadHandler(a *app.App, userID int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		days := 7
		if v := c.Query("days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 90 {
				days = n
			}
		}
		var (
			week         []store.UsagePoint
			weekModels   []store.UsageModelPoint
			modelStats   []store.ModelUsage
			modelPrev    []store.ModelUsage
			hours        []store.UsageHourPoint
			channelStats []store.ChannelDayStats
			today        store.UsageSummary
			fails        []*models.RequestLog
			err          error
		)
		now := time.Now()
		dayStartT := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		curFrom := dayStartT.AddDate(0, 0, -(days - 1)).Unix()
		prevFrom := dayStartT.AddDate(0, 0, -(2*days - 1)).Unix()
		if userID > 0 {
			week, err = store.UserUsageByDay(a.DB, userID, days)
			if err == nil {
				weekModels, err = store.UserUsageModelByDay(a.DB, userID, days)
			}
			if err == nil {
				modelStats, err = store.UserUsageByModel(a.DB, userID, curFrom, 0)
			}
			if err == nil {
				modelPrev, err = store.UserUsageByModel(a.DB, userID, prevFrom, curFrom)
			}
			if err == nil {
				hours, err = store.UserUsageByHour(a.DB, userID, 24)
			}
			if err == nil {
				today, err = store.UserUsageSummary(a.DB, userID, dayStartT.Unix())
			}
			if err == nil {
				channelStats, err = store.UserChannelStatsToday(a.DB, userID)
			}
			if err == nil {
				fails, err = store.UserRecentFailures(a.DB, userID, 10)
			}
		} else {
			week, err = store.UsageByDay(a.DB, days)
			if err == nil {
				weekModels, err = store.UsageModelByDay(a.DB, days)
			}
			if err == nil {
				modelStats, err = store.UsageByModel(a.DB, curFrom, 0)
			}
			if err == nil {
				modelPrev, err = store.UsageByModel(a.DB, prevFrom, curFrom)
			}
			if err == nil {
				hours, err = store.UsageByHour(a.DB, 24)
			}
			if err == nil {
				today, err = store.UsageSummaryRange(a.DB, dayStartT.Unix())
			}
			if err == nil {
				channelStats, err = store.ChannelStatsToday(a.DB)
			}
			if err == nil {
				fails, err = store.RecentFailures(a.DB, 10)
			}
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": dashboardPayload{
			Days: days,
			Week: week, WeekModels: weekModels, Hours: hours,
			Models: modelStats, ModelsPrev: modelPrev,
			Today:        today,
			ChannelStats: channelStats, Failures: fails, RecentFailures: len(fails),
		}})
	}
}

// RegisterSettingRoutes 挂载系统设置。
func RegisterSettingRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", getSettings(a))
	g.PUT("", updateSettings(a))
}

// SettingItem 系统设置项：按 classification, key, val, description 顺序序列化。
type SettingItem struct {
	Classification string `json:"classification"`
	Key            string `json:"key"`
	Val            string `json:"val"`
	Description    string `json:"description"`
}

// settingDef 设置目录中的一条：分类 / key / 描述 / 默认值 / 是否可改。
type settingDef struct {
	classification string
	key            string
	description    string
	def            string // 未设置时透出的默认值
	mutable        bool
}

// settingsCatalog 内置设置目录：每项设置都有分类与描述，字段顺序即返回顺序。
var settingsCatalog = []settingDef{
	{classification: "Basic", key: "default_timeout_ms", description: "Default request timeout (ms) used when a channel has no per-channel timeout", def: "60000", mutable: true},
	{classification: "Logging", key: "log_retain_days", description: "Request logs older than this many days are cleaned up on startup (0 = disabled)", def: "30", mutable: true},
	{classification: "Auto-pull", key: "pull_interval_min", description: "Interval for auto-pulling upstream models (minutes); 0 = disabled", def: "60", mutable: true},
	{classification: "Auto-pull", key: "pull_default_priority", description: "Default priority for routes created by auto-pull (lower = preferred)", def: "0", mutable: true},
	{classification: "Auto-pull", key: "pull_default_weight", description: "Default weight for routes created by auto-pull", def: "1", mutable: true},
	{classification: "Runtime", key: "gateway_prefix", description: "Gateway prefix (runtime config, read-only)", def: "/", mutable: false},
	{classification: "Failover", key: "circuit_fail_threshold", description: "Circuit breaker: consecutive failures within the window before a channel/model is cooled down (jitter resistance)", def: "6", mutable: true},
	{classification: "Failover", key: "circuit_fail_window_s", description: "Circuit breaker: sliding window (seconds) for consecutive-failure counting", def: "30", mutable: true},
	{classification: "Failover", key: "circuit_quota_cooldown_s", description: "Circuit breaker: cooldown (seconds) when a key hits quota/arrears/expiry", def: "30", mutable: true},
	{classification: "Failover", key: "circuit_probe_backoff_s", description: "Probe: backoff (seconds) after a failed recovery probe of a cooled-down channel", def: "30", mutable: true},
	{classification: "Rate limit", key: "rate_limit_qps", description: "Per-user gateway requests per second (0 = disabled)", def: "0", mutable: true},
	{classification: "Rate limit", key: "rate_limit_concurrency", description: "Per-user max concurrent in-flight gateway requests (0 = disabled)", def: "0", mutable: true},
}

func getSettings(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		out := make([]SettingItem, 0, len(settingsCatalog))
		for _, d := range settingsCatalog {
			item := SettingItem{Classification: d.classification, Key: d.key, Description: d.description}
			if d.key == "gateway_prefix" {
				// 网关前缀为运行期配置（LGM_GATEWAY_PREFIX），只读透出给前端拼 gateway base URL
				item.Val = a.Cfg.GatewayPrefix
			} else {
				v, err := a.GetSetting(d.key)
				if err != nil && err != sql.ErrNoRows {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				if v == "" {
					v = d.def
				}
				item.Val = v
			}
			out = append(out, item)
		}
		c.JSON(http.StatusOK, gin.H{"data": out})
	}
}

func updateSettings(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		var b struct {
			Data []SettingItem `json:"data"`
		}
		if !decode(c, &b) {
			return
		}
		mutable := make(map[string]bool, len(settingsCatalog))
		for _, d := range settingsCatalog {
			if d.mutable {
				mutable[d.key] = true
			}
		}
		for _, item := range b.Data {
			if !mutable[item.Key] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "setting is not mutable: " + item.Key})
				return
			}
		}
		for _, item := range b.Data {
			if err := a.SetSetting(item.Key, item.Val); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"data": "saved"})
	}
}
