# 部署指南

llmgate 是一个**单容器**应用（Go 后端 + 内嵌 Vue 前端），部署极简。推荐 Docker Compose 方式，适合 VPS / NAS / 家宽小主机。

## 一、Docker Compose 部署（推荐）

### 前置条件
- Linux 服务器，已安装 Docker Engine + Compose v2（`docker compose version` 有输出即可）
- git

### 第 1 步：获取代码
```bash
git clone <你的仓库地址> llmgate
cd llmgate
```

### 第 2 步：生成 `.env`
```bash
cp .env.example .env
# 生成随机值并填入：
#   LGM_ADMIN_TOKEN=$(openssl rand -hex 24)   # 管理后台登录用
#   LGM_ENCRYPT_SEED=$(openssl rand -hex 32)  # API Key 加密种子；一旦使用请妥善保存
```
```bash
vim .env   # 填 LGM_ADMIN_TOKEN 与 LGM_ENCRYPT_SEED
```

### 第 3 步：构建并启动
```bash
make docker          # = docker compose up -d --build
# 首次构建较慢（node + go 编译），之后增量很快
```

### 第 4 步：验证
```bash
curl http://<服务器IP>:3210/healthz        # → {"status":"ok"}
curl -o /dev/null -w '%{http_code}\n' http://<服务器IP>:3210/   # → 200（管理后台）
```

### 第 5 步：初始化使用
1. 浏览器打开 `http://<服务器IP>:3210` ，用 `LGM_ADMIN_TOKEN` 登录管理后台
2. 「渠道管理」新建上游套餐（BaseURL + API Key，可多个 Key）
3. 「模型路由」把对外模型名绑定渠道
4. 「用户管理」创建用户，得到一次性 `sk-xxx` 令牌
5. 在任意 OpenAI 兼容客户端中配置：
   - **Base URL**: `http://<服务器IP>:3210/v1`
   - **API Key**: `sk-xxx`

## 二、常用运维

| 操作 | 命令 |
|---|---|
| 查看状态 | `docker compose ps` / `docker compose logs -f --tail=100` |
| 停止 | `make down` |
| 升级（拉代码后重建） | `git pull && make docker` |
| 手动重启 | `make up` |

### 数据备份（重要）
所有数据都在宿主机 `./data/llmgate.db`（SQLite 单文件，WAL 模式）。
备份=复制这几个文件（建议先停服务或用 `sqlite3 .backup`）：
```bash
# 热备份（推荐）
docker compose exec llmgate sh -c "cp /data/llmgate.db /data/llmgate.db.bak"
# 或冷备份
make down && cp data/llmgate.db ~/llmgate-backup-$(date +%F).db && make up
```
恢复：停服务 → 用备份文件覆盖 `data/llmgate.db` → 启动。

> ⚠️ 若 `LGM_ENCRYPT_SEED` 是自动生成并被持久化在 DB 的 `settings` 表里，**备份 DB 即等于备份了种子**。不要把 DB 和 `.env` 分开丢一半。

## 三、可选：Nginx 反向代理 + HTTPS

不想直接暴露 3210 端口时，用 Nginx + Let's Encrypt 挂 HTTPS：

```nginx
server {
    listen 443 ssl;
    server_name llm.example.com;
    ssl_certificate     /etc/letsencrypt/live/llm.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/llm.example.com/privkey.pem;

    client_max_body_size 16m;

    location / {
        proxy_pass http://127.0.0.1:3210;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        # SSE（Phase 2 流式启用后需要）
        proxy_buffering off;
        proxy_read_timeout 3600s;
    }
}
```
之后客户端 Base URL 改为 `https://llm.example.com/v1`。

### 防火墙
只需放行你实际对外服务的端口（默认映射 `TCP 3210`，或用 Nginx 时只放行 80/443）。

## 四、备选：裸二进制 + systemd（不依赖 Docker）

适合已有机器不想装 Docker 的情况：

```bash
# 在一台有 Go 1.27+ 的机器上构建（或 CI 产物）
sh scripts/build-web.sh && CGO_ENABLED=0 go build -tags embedweb -o llmgate ./cmd/server

# 拷贝二进制到服务器，创建运行用户与目录
sudo useradd -r -s /sbin/nologin llmgate
sudo mkdir -p /opt/llmgate/data && sudo chown -R llmgate:llmgate /opt/llmgate

# systemd 服务 /etc/systemd/system/llmgate.service
[Unit]
Description=llmgate LLM gateway
After=network.target

[Service]
User=llmgate
ExecStart=/opt/llmgate/llmgate
WorkingDirectory=/opt/llmgate
Environment=LGM_DB_PATH=/opt/llmgate/data/llmgate.db
Environment=LGM_HTTP_ADDR=:3210
Environment=LGM_ADMIN_TOKEN=换成你的令牌
Environment=LGM_ENCRYPT_SEED=换成你的种子

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now llmgate
```

## 五、常见问题

| 现象 | 处理 |
|---|---|
| 忘记管理后台令牌 | 看 `docker compose logs` 中首次启动打印的令牌；或改 `.env` 的 `LGM_ADMIN_TOKEN` 后清空 settings 的 `admin_token_hash`（首次启动种子未变时直接删掉 `data/llmgate.db` 会同时丢所有配置，慎用） |
| 端口被占用 | 改 `.env` 的 `LGM_HTTP_ADDR` 与 compose 的 `ports` 映射保持一致 |
| 容器反复重启/日志报 DB 权限 | entrypoint 会自动 chown `/data`；若用了其余挂载方式，确保运行用户可写 |
| 网关返回 404 | 该模型没有启用且健康的模型路由，先到后台「模型路由 + 渠道状态」检查 |
| 客户端连不上 | 确认防火墙放行、`LGM_GATEWAY_PREFIX` 是否为默认 `/`（客户端 Base URL 需含 `/v1`） |