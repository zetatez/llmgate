//go:build embedweb

// Package web 托管前端静态资源。
// 使用 -tags embedweb 构建时，将 web/dist 嵌入二进制并作为静态站点托管。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler 返回托管前端静态资源的 http.Handler（SPA 回退到 index.html）。
// 若 dist 缺失 index.html（未构建前端），返回 nil，服务不托管静态资源。
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if _, err := fs.Stat(sub, p); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA 路由 / 未命中：回退 index.html。index.html 不缓存，
		// 避免浏览器长期持有旧版页面（旧按钮调用已下线接口）。
		w.Header().Set("Cache-Control", "no-store")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
