# llmgate

LLM 套餐聚合网关：把多个上游大模型套餐聚合成一个 **OpenAI 兼容** 接口。自用为主、可轻量分享给朋友。

- 后端：Go + Gin + 原生 SQL（`database/sql`，无 ORM）+ SQLite（WAL）
- 前端：Vue 3 + Vite + TypeScript + Element Plus + ECharts（英文界面）
- 部署：单容器镜像（前端嵌入 Go 二进制），docker-compose 一键起；默认时区 Asia/Shanghai

## 功能

**渠道管理**
- 每个上游套餐一个渠道（BaseURL / 优先级 / 权重 / 超时 / 备注）
- **多 Key 号池**：每个号可命名（账号）、加备注、独立启停、设权重、随时**更换密钥**
- AES-256-GCM 加密存储、连通性测试、渠道/号**快捷启停开关**
- **有效状态**：渠道停用 → 其下所有号与路由立即失效（不参与转发），但号/路由自身的启用值不变，重启渠道即整体恢复

**模型路由**
- 对外模型名 ↔ 多渠道路由，**自动同步**：定时（默认 60 分钟，可配）拉取各渠道 `/v1/models`，自动新建模型映射、**自动清理已下架的陈旧路由**（手动别名路由保留）；渠道页提供 **Sync All Routes** 立即同步
- **优先级 + 权重**：有效优先级 =（路由优先级, 渠道优先级），高优先级先消费；同层级内按「路由权重 × 渠道权重」比例分发
- **熔断故障转移**：高优先级被限流(429)/余额用尽/5xx/超时 → 自动下沉低优先级；失败渠道内存熔断，退避阶梯 **30s → 60s → 15min**，过期自动回探恢复——避免每条请求都白打失败渠道

**网关**
- `/v1/chat/completions`、`/v1/completions`、`/v1/embeddings`、`/v1/models`
- 每个用户独立 `sk-xxx` 令牌鉴权 + 额度上限；网关 `gateway_prefix + /v1`

**用量与费用**
- 请求日志批量异步落库（WAL + 缓冲，不阻塞网关）
- 仪表盘：今日指标卡 / 近 7 天趋势 / 渠道今日状态 / 最近失败明细
- **按用户 × 模型**用量（请求次数 / 上传(输入) / 下载(输出) token / 费用）
- **模型单价表**：联网调研生成 190+ 模型定价（`docs/model-pricing.json`），日志与仪表盘实时估算美元费用，可在设置页改

**管理后台**（Dashboard / Channels / Model Routes / Users / Logs / Settings）
- 用户页：Status 开关、API Key 列（掩码+尾号）**随时复制**、一键 Reset 并复制、Base URL 展示

> 流式响应（SSE）暂未实现（`adapter.Adapter.DoStream` 已预留）。

## 快速开始

### Docker Compose（推荐）

```bash
cp .env.example .env        # 填入 LGM_ADMIN_TOKEN（openssl rand -hex 24）
docker compose up -d --build
# 打开 http://localhost:3210 （管理员令牌见 .env）
```

### 本地开发

```bash
# 终端 1：后端（自动读取 ./.env，监听 .env 里配置的端口，默认 :3210）
make dev-backend

# 终端 2：前端（vite 代理 /api 与 /v1 到后端）
make dev-web
```

生产二进制（前端嵌入）`make build` → `bin/llmgate`；升级：`git pull && make docker`。

详细部署（Docker/裸二进制/nginx HTTPS/备份）见 **[docs/deploy.md](docs/deploy.md)**。

## 使用流程

1. 管理后台登录（`LGM_ADMIN_TOKEN`）
2. **Channels** → 新增上游渠道（BaseURL + 一个或多个号），点 **Test** 连通；点 **Sync All Routes** 自动拉取该渠道全部模型并建映射
3. **Model Routes** → 确认映射；对主动渠道可再配**优先级/权重**
4. **Users** → 建用户拿令牌；把 Base URL 与 `sk-xxx` 发给使用者
5. 客户端调用：

```bash
curl http://localhost:3210/v1/chat/completions \
  -H "Authorization: Bearer sk-你的令牌" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"你好"}]}'
```

## 配置（环境变量 `.env`）

| 变量 | 默认 | 说明 |
|---|---|---|
| `LGM_ADMIN_TOKEN` | 首次自动生成 | 管理端令牌（每次启动以此为准，改后重启即可换登录） |
| `LGM_ENCRYPT_SEED` | 自动生成 | API Key/令牌加密种子（32 字节 hex），持久化于 settings |
| `LGM_HTTP_ADDR` | `:3210` | 监听地址 |
| `LGM_DB_PATH` | `./data/llmgate.db` | SQLite 路径（容器内固定 `/data/llmgate.db`） |
| `LGM_GATEWAY_PREFIX` | `/` | 开放网关前缀（拼 `/v1`） |
| `LGM_LOG_LEVEL` | `release` | gin 日志级别 |
| `LGM_TZ` | `Asia/Shanghai` | 应用时区（SQLite 与 Go 时间口径一致） |

设置页内的运行期配置：自动拉取间隔(默认 60min/0=禁用)、默认映射优先级/权重、模型单价表、日志保留天数、默认超时。

## 数据与备份

- SQLite 单文件 `data/llmgate.db`（WAL 模式），备份=复制整个 `data/` 目录；表结构由内嵌迁移脚本按版本自动升级（`internal/store/migrations/`）
- **务必定期备份**：`make down && cp -r data data.bak && make up`（或 `sqlite3 data/llmgate.db ".backup ..."` 热备）

## 目录结构

```
cmd/server/           入口（时区、日志总线、定时同步任务）
internal/
  config/             环境配置（含 .env 加载）
  store/              database/sql + 原生 SQL、迁移脚本
  secret/             AES-256-GCM 加解密
  adapter/openai/     上游 OpenAI 兼容协议适配器
  router/             优先级/权重选择、Key 池、熔断故障转移
  modelpull/          上游模型同步（建新 + 清陈旧，幂等）
  puller/             定时自动同步任务
  gateway/            /v1/* 开放网关
  apiv1/              /api/admin/* 管理端
  logbus/             请求日志异步批量落库
  quota/              成本估算
  web/                内嵌前端（-tags embedweb）
web/                  Vue 前端源码
docs/                 design.md 设计文档 / deploy.md 部署指南 / model-pricing.json 单价表
```

## Roadmap

- [x] Phase 1：渠道/号池/模型路由 CRUD、非流式转发、熔断故障转移、日志统计、管理后台、Docker 部署
- [x] 增强：自动模型同步（定时+手动）、单价费用估算、渠道优先级/权重生效、东八区、令牌随时复制
- [x] Phase 2：**SSE 流式转发**（首字节前故障转移、中断错误事件、用量捕获）、**DB 级冷却持久化 + 探活自愈**、**日志保留自动清理**、**单元测试**（quota/secret/router/store）
- [ ] Phase 3（按需）：其他协议适配器（Anthropic/Gemini）、Prometheus 指标、响应缓存、告警

详见 [docs/design.md](docs/design.md) 与 [docs/deploy.md](docs/deploy.md)。
