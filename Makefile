# Makefile 常用命令
.PHONY: dev-backend dev-web build build-web docker up down test clean

dev-backend: ## 本地开发：启动后端（HOT，无前端嵌入）
	go run ./cmd/server

dev-web: ## 本地开发：启动前端（vite，代理 /api 与 /v1 到 :8080）
	cd web && npm run dev

build-web: ## 构建前端产物到 web/dist
	sh scripts/build-web.sh

build: build-web ## 本地完整构建（嵌入前端），产物 bin/llmgate
	go build -tags embedweb -o bin/llmgate ./cmd/server

up: ## 启动容器（构建镜像）
	docker compose up -d --build

down: ## 停止容器
	docker compose down

restart: ## 重构启动容器
	docker compose down
	docker compose up -d --build

test: ## 运行 Go 测试
	go test ./...

clean: ## 清理产物
	rm -rf bin internal/web/dist web/dist
