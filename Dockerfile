# syntax=docker/dockerfile:1

# ---- 构建前端 ----
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
# npm 下载缓存跨构建复用；仅 package 文件变化才重新 npm ci（且命中缓存，秒级）
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

# ---- 构建后端（嵌入前端产物） ----
FROM golang:1.27-alpine AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
# go 模块缓存与编译缓存跨构建复用：本地仅改了少量代码时，go build 走增量编译
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist internal/web/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -tags embedweb -ldflags="-s -w" -o /out/llmgate ./cmd/server

# ---- 运行镜像 ----
FROM alpine:3.20
RUN apk add --no-cache su-exec tzdata && adduser -D -u 10001 llmgate
WORKDIR /app
COPY --from=gobuild /out/llmgate /app/llmgate
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh && mkdir -p /data
ENV LGM_DB_PATH=/data/llmgate.db \
    LGM_HTTP_ADDR=:3210 \
    LGM_TZ=Asia/Shanghai \
    TZ=Asia/Shanghai
EXPOSE 3210
VOLUME ["/data"]
ENTRYPOINT ["/docker-entrypoint.sh"]