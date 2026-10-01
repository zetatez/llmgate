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
				c.JSON(http.StatusBadRequest, gin.H{"error": "date 格式应为 YYYY-MM-DD"})
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
		f := store.LogFilter{
			DisplayModel: c.Query("display_model"),
			Status:       c.Query("status"),
		}
		if v := c.Query("channel_id"); v != "" {
			id, _ := strconv.ParseInt(v, 10, 64)
			f.ChannelID = id
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
		list, err := store.QueryLogs(a.DB, f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
	}
}

// dashboardPayload 仪表盘一次性数据。
type dashboardPayload struct {
	Week           []store.UsagePoint      `json:"week"`
	Today          store.UsageSummary      `json:"today"`
	ChannelStats   []store.ChannelDayStats `json:"channel_stats"`
	Failures       []*models.RequestLog    `json:"failures"` // 最近失败明细
	RecentFailures int                     `json:"recent_failures"`
}

func usageDashboard(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		week, err := store.UsageByDay(a.DB, 7)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		dayStart := func() int64 {
			now := time.Now()
			return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
		}
		today, err := store.UsageSummaryRange(a.DB, dayStart())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total, err := store.UsageSummaryRange(a.DB, 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		_ = total // 累计统计不再输出到仪表盘（取舍：对日常使用无指导意义）

		channelStats, err := store.ChannelStatsToday(a.DB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		fails, err := store.RecentFailures(a.DB, 10)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": dashboardPayload{
			Week: week, Today: today, ChannelStats: channelStats, Failures: fails, RecentFailures: len(fails),
		}})
	}
}

// RegisterSettingRoutes 挂载系统设置。
func RegisterSettingRoutes(g *gin.RouterGroup, a *app.App) {
	g.GET("", getSettings(a))
	g.PUT("", updateSettings(a))
}

// SettingItem 允许修改的系统设置。
type SettingItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var mutableSettings = map[string]bool{
	"default_timeout_ms":    true,
	"log_retain_days":       true,
	"model_pricing":         true, // JSON: {"deepseek-chat": {"input":0.14,"output":0.28}}
	"pull_interval_min":     true, // 自动拉取间隔（分钟），0 = 禁用
	"pull_default_priority": true, // 自动拉取新建路由的默认优先级
	"pull_default_weight":   true, // 自动拉取新建路由的默认权重
}

// readOnlySettings 只读展示、不可修改的设置。
var readOnlySettings = []string{"model_pull_last"}

func getSettings(a *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys := make([]string, 0, len(mutableSettings)+len(readOnlySettings))
		for k := range mutableSettings {
			keys = append(keys, k)
		}
		keys = append(keys, readOnlySettings...)
		out := []SettingItem{}
		for _, k := range keys {
			v, err := a.GetSetting(k)
			if err != nil && err != sql.ErrNoRows {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if v == "" {
				switch k {
				case "default_timeout_ms":
					v = "60000"
				case "log_retain_days":
					v = "30"
				case "pull_interval_min":
					v = "60"
				case "pull_default_priority":
					v = "0"
				case "pull_default_weight":
					v = "1"
				}
			}
			out = append(out, SettingItem{Key: k, Value: v})
		}
		// 网关前缀为运行期配置（LGM_GATEWAY_PREFIX），只读透出给前端拼 gateway base URL
		out = append(out, SettingItem{Key: "gateway_prefix", Value: a.Cfg.GatewayPrefix})
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
		for _, item := range b.Data {
			if !mutableSettings[item.Key] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "不允许修改的配置: " + item.Key})
				return
			}
		}
		for _, item := range b.Data {
			if err := a.SetSetting(item.Key, item.Value); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"data": "saved"})
	}
}
