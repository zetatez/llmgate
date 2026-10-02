// llmgate 入口：加载配置、初始化数据库、启动 HTTP 服务。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
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

	a, err := app.New(cfg, db)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}

	logBus := logbus.New(db, 1024)
	defer logBus.Close()

	// 共享熔断器：Router（网关）与 Puller（探活自愈）共用一个实例
	pen := router.NewPenalizer()

	// 定时自动拉取上游模型 + 冷却探活 + 日志清理
	modelPuller := puller.New(a, pen)
	go modelPuller.Run()
	go modelPuller.RunBackground()

	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	apiv1.Register(r.Group("/api/admin"), a, modelPuller.SyncNow)
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

	// 优雅停机：收到 SIGTERM/SIGINT 后先通知网关（流式 handler 尽早补发失败终态），
	// 再停止接收新连接，给在途请求最多 grace 时间自然结束；超时后强关连接。
	drainCtx, drainCancel := context.WithCancel(context.Background())
	gateway.SetDrainContext(drainCtx)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	drainCancel()
	const grace = 10 * time.Second
	log.Printf("shutting down: draining in-flight requests (grace %s)", grace)
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: drain timed out (%v); force-closing remaining connections", err)
		_ = srv.Close()
	}
	log.Println("server stopped")
}
