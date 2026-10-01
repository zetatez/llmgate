//go:build !embedweb

// Package web 托管前端静态资源。
// 默认构建（无 embedweb tag）不嵌入前端，便于本地开发（由 vite dev server 代理）。
package web

import "net/http"

// Handler 返回 nil：未启用前端嵌入。
func Handler() http.Handler { return nil }
