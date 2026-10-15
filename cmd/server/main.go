// llmgate 入口：加载配置、初始化数据库、启动 HTTP 服务。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // 内嵌时区数据，容器无系统 tzdata 也能 LoadLocation

	"github.com/gin-gonic/gin"

	"llmgate/internal/adapter/openai" // init() 注册适配器；SetProxy 配置出站代理
	"llmgate/internal/apiv1"
	"llmgate/internal/app"
	"llmgate/internal/config"
	"llmgate/internal/gateway"
	"llmgate/internal/logbus"
	"llmgate/internal/puller"
	"llmgate/internal/router"
	"llmgate/internal/store"
	"llmgate/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 设置应用时区（SQLite 'localtime' 依赖进程 TZ 环境变量，需与 LGM_TZ 保持一致）
	if loc, err := time.LoadLocation(cfg.TZ); err == nil {
		_ = os.Setenv("TZ", cfg.TZ)
		time.Local = loc
		log.Printf("timezone: %s", cfg.TZ)
	} else {
		log.Printf("warn: invalid LGM_TZ=%q: %v", cfg.TZ, err)
	}

	// 上游出站代理（socks5/http），用于绕过网络过滤
	if err := openai.SetProxy(cfg.Proxy); err != nil {
		log.Fatalf("proxy config: %v", err)
	}
	if cfg.Proxy != "" {
		log.Printf("upstream proxy: %s", cfg.Proxy)
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := store.Migrate(db); err != nil {
		log.Fatalf("migrate db: %v", err)
	}
	if err := store.CleanupExpiredSessions(db); err != nil {
		log.Printf("warn: cleanup expired sessions: %v", err)
	}

	a, err := app.New(cfg, db)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}

	logBus := logbus.New(db, 1024)
	defer logBus.Close()

	// 共享熔断器：Router（网关）与 Puller（探活自愈）共用一个实例
	pen := router.NewPenalizer()
	// 用 settings 覆盖熔断参数（阈值/窗口/配额冷却），非法或空值回退默认
	applyPenaltyTuning(a, pen)

	// 定时自动拉取上游模型 + 冷却探活 + 日志清理
	modelPuller := puller.New(a, pen)
	go modelPuller.Run()
	go modelPuller.RunBackground()

	// 会话清理：过期会话每小时回收（启动时已清理一次）
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := store.CleanupExpiredSessions(db); err != nil {
				log.Printf("warn: cleanup sessions: %v", err)
			}
		}
	}()

	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	// 受信代理：留空=不信任 X-Forwarded-For（ClientIP 取 TCP 对端，防伪造绕过登录限速）。
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		log.Fatalf("set trusted proxies: %v", err)
	}
	// 基础安全响应头
	r.Use(func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		c.Next()
	})

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	apiv1.Register(r.Group("/api/admin"), a, modelPuller.SyncNow)
	apiv1.RegisterUserPortal(r.Group("/api/user"), a)
	r.GET("/api/session", apiv1.SessionProbe(a))
	gateway.Register(r.Group(cfg.GatewayPrefix+"v1"), a, logBus, pen)

	// 生产构建（-tags embedweb）时托管前端静态资源；本地开发返回 nil 不影响运行
	if h := web.Handler(); h != nil {
		r.NoRoute(func(c *gin.Context) {
			// 非页面请求（如已下线的旧接口）返回标准 404 JSON，避免误回 index.html
			if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
				c.JSON(http.StatusNotFound, gin.H{"error": "api not found"})
				return
			}
			h.ServeHTTP(c.Writer, c.Request)
		})
		log.Println("web static assets embedded and served")
	}

	log.Printf("llmgate listening on %s (gateway prefix %s)", cfg.HTTPAddr, cfg.GatewayPrefix+"v1")

	// 超时硬化：ReadHeaderTimeout 防 slowloris 占满连接；IdleTimeout 回收空闲连接。
	// 注意不设 ReadTimeout/WriteTimeout——前者会掐断长请求体（大 prompt），
	// 后者会掐断长 SSE 流式响应（我们已在适配器层用空闲超时控制，勿在此误杀）。
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// 优雅停机：收到 SIGTERM/SIGINT 后不再接收新请求，等待在途请求（含长流式）自然结束，
	// 而不是掐断；健康流靠各自「空闲超时」兜底。grace 秒后仍未排空则强关（应小于 docker
	// compose 的 stop_grace_period，避免被 docker 抢先 SIGKILL）。
	grace := shutdownGraceSec()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Printf("shutting down: stopping new connections; waiting for in-flight requests to finish naturally (grace %ds)", grace)
	sctx, cancel := context.WithTimeout(context.Background(), time.Duration(grace)*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		log.Printf("shutdown: grace %ds exceeded (%v); force-closing remaining connections", grace, err)
		_ = srv.Close()
	}
	log.Println("server stopped")
}

// shutdownGraceSec 停机等待秒数：默认 280（小于 compose 的 stop_grace_period 300s），
// 可通过 LGM_SHUTDOWN_GRACE 覆盖。
func shutdownGraceSec() int {
	n := 280
	if v := os.Getenv("LGM_SHUTDOWN_GRACE"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x > 0 {
			n = x
		}
	}
	return n
}

// applyPenaltyTuning 用 settings 里的熔断参数覆盖共享 Penalizer 的默认值。
// 空/非法值回退默认；仅启动时应用一次（后台探活退避则每次探活热读）。
func applyPenaltyTuning(a *app.App, pen *router.Penalizer) {
	def := router.DefaultTuning()
	t := router.Tuning{
		FailThreshold: intSettingSec(a, "circuit_fail_threshold", def.FailThreshold),
		FailWindow:    time.Duration(intSettingSec(a, "circuit_fail_window_s", int(def.FailWindow/time.Second))) * time.Second,
		QuotaPenalty:  time.Duration(intSettingSec(a, "circuit_quota_cooldown_s", int(def.QuotaPenalty/time.Second))) * time.Second,
	}
	pen.ApplyTuning(t)
	log.Printf("circuit-breaker tuning: fail_threshold=%d fail_window=%v quota_cooldown=%v",
		t.FailThreshold, t.FailWindow, t.QuotaPenalty)
}

// intSettingSec 读取一个正整数 settings；空/非法/非正回退默认。
func intSettingSec(a *app.App, key string, def int) int {
	v, err := a.GetSetting(key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
