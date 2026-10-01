# llmgate 设计文档

> LLM 套餐聚合网关：把多个上游大模型套餐聚合成一个 OpenAI 兼容接口，自用为主、可轻量分享给朋友。

- 后端：Go + Gin
- 前端：Vue 3 + Vite + TypeScript + Element Plus
- 部署：单容器（后端 + 前端静态文件打成一个镜像），docker-compose 一键起，SQLite 单库

## 1. 定位与目标

### 核心目标
- 把多个「上游套餐」（渠道）统一聚合成一个 OpenAI 兼容的网关接口
- 模型名聚合：对外一个模型名可映射到多个上游渠道，自动路由与故障转移
- 可用性优先：主用渠道失败自动切换备用渠道，对调用方透明
- 自用为主，可给少量朋友发独立令牌并设定额度

### 非目标（当前明确不做）
- 多租户计费/充值/账单（只做配额上限）
- 其他上游协议适配器（Claude/Gemini 原生协议）— 预留扩展点，Phase 2 后再议
- 高可用多实例 / 分布式（单机社区版，SQLite）

## 2. 整体架构

```
你的工具/客户端 ──OpenAI协议(SSE)──▶ llmgate (单个容器)
                                       │
              ┌────────────────────────┼─────────────────────────┐
              │  gateway  /v1/*        │  router 路由与故障转移  │
              │  鉴权/限流/SSE         │  model_routes → channels│
              └────────────────────────┴─────────────────────────┘
                                       │
                     OpenAI 兼容适配器 │
                                       ▼
                    [渠道A: 套餐1] [渠道B: 套餐2] [渠道C: 套餐3]
                        (各渠道有多 Key 池，权重轮换)

llmgate 内部还提供 /api/admin/* 管理 API + Vue 管理后台(静态资源由同一进程托管)
```

## 3. 技术栈

 | 层       | 选型                                                                                                        |
 | ---      | ---                                                                                                         |
 | 后端     | Go 1.22+、Gin、`database/sql`（标准库原生 SQL）+ sqlite（modernc 纯 Go 驱动，免 CGO）、SSE 手写             |
 | 数据库   | SQLite（WAL 模式 + busy_timeout）；**全部走原生 SQL，不用 ORM**，DDL 由内嵌迁移脚本管理（启动时按版本执行） |
 | Key 加密 | AES-256-GCM，种子存 `settings` 表，可用环境变量覆盖                                                         |
 | 前端     | Vue 3 + Vite + TypeScript + Element Plus + ECharts + Pinia + Vue Router                                     |
 | 部署     | 单 Dockerfile 多阶段构建（前端产物 embed 进 Go 二进制或同一镜像 Nginx 托管），docker-compose 挂数据卷       |
 | 配置     | `.env`：管理员令牌、SQLite 路径、监听端口、加密种子                                                         |

## 4. 目录结构

```
llmgate/
├── cmd/server/main.go          # 入口：HTTP server、路由挂载、启动任务
├── internal/
│   ├── models/                 # 表结构对应的 Go struct（不含数据库逻辑）
│   ├── store/                  # 数据访问层（database/sql + 原生 SQL 查询/事务）
│   │   └── migrations/         # 内嵌 SQL 迁移脚本（按版本号执行）
│   ├── adapter/                # 上游协议适配层（现仅 openai 兼容，接口化便于扩展）
│   │   ├── adapt.go            # Adapter 接口 + 注册
│   │   └── openai/             # chat/completions/embeddings 兼容实现
│   ├── router/                 # 核心：模型路由选择、健康状态、故障转移
│   ├── gateway/                # /v1/* 开放网关：鉴权、限流、转发、SSE
│   ├── quota/                  # 额度计算、成本估算、用量统计
│   ├── apiv1/                  # /api/admin/* 管理端
│   └── web/                    # 内嵌前端静态资源
├── web/                        # Vue 前端源码
├── docs/design.md              # 本文档
└── docker-compose.yml
```

## 5. 数据模型（SQLite）

### users（轻量用户）
 | 字段          | 类型   | 说明                                                        |
 | ---           | ---    | ---                                                         |
 | id            | int    | PK                                                          |
 | name / remark | string | 显示名/备注                                                 |
 | token_hash    | string | 令牌哈希存储（sha256），原始 `sk-xxx` 仅创建/重置时展示一次 |
 | quota_limit   | float  | 额度上限（按估算成本美元计），0 = 不限                      |
 | quota_used    | float  | 已用额度（累计）                                            |
 | status        | int    | 启用/禁用                                                   |

### channels（渠道 = 一个上游套餐）
 | 字段         | 类型   | 说明                                   |
 | ---          | ---    | ---                                    |
 | id           | int    | PK                                     |
 | name         | string | 渠道名，如 “DeepSeek 官方”             |
 | base_url     | string | 上游 base url                          |
 | adapter      | string | 协议类型，现固定 `openai`              |
 | priority     | int    | 路由优先级，越小越优先                 |
 | weight       | int    | 同优先级时的加权随机权重               |
 | timeout_ms   | int    | 单请求超时                             |
 | enabled      | bool   | 是否参与路由                           |
 | health_state | string | healthy / cooldown（连续失败自动冻结） |
 | note         | string | 备注                                   |

### channel_keys（一个渠道挂多个 Key）
 | 字段         | 类型 | 说明                   |
 | ---          | ---  | ---                    |
 | id           | int  | PK                     |
 | channel_id   | int  | FK → channels          |
 | api_key_enc  | blob | AES-256-GCM 加密的 Key |
 | enabled      | bool | 是否参与使用           |
 | weight       | int  | 池内轮换权重           |
 | last_used_at | time | 最近使用时间           |

### model_routes（对外模型名 → 渠道映射）
 | 字段           | 类型   | 说明                           |
 | ---            | ---    | ---                            |
 | id             | int    | PK                             |
 | display_name   | string | 对外模型名，如 `deepseek-chat` |
 | channel_id     | int    | FK → channels                  |
 | upstream_model | string | 上游真实模型名                 |
 | priority       | int    | 该映射在模型内的优先级         |
 | weight         | int    | 同优先级加权随机               |
 | enabled        | bool   | 是否参与路由                   |

> 索引：`(display_name)`；一个对外模型名可有 N 条映射记录（=N 个备用渠道）。

### request_logs（每次转发记录）
 | 字段                                             | 类型   | 说明                                           |
 | ---                                              | ---    | ---                                            |
 | id                                               | int    | PK                                             |
 | user_id                                          | int    | FK → users                                     |
 | display_model                                    | string | 对外模型名                                     |
 | channel_id / key_id                              | int    | 实际使用的渠道与 Key                           |
 | upstream_model                                   | string | 上游真实模型                                   |
 | prompt_tokens / completion_tokens / total_tokens | int    | 用量（转发的响应里解析，流式按 finish 后上报） |
 | cost                                             | float  | 成本估算                                       |
 | latency_ms                                       | int    | 总耗时（含重试）                               |
 | status                                           | string | success / error                                |
 | error_code                                       | string | 失败原因（timeout / 429 / 5xx / network…）     |
 | stream                                           | bool   | 是否流式请求                                   |
 | created_at                                       | time   | 索引：`(created_at)`、`(display_model)`        |

> 写入策略：请求日志**批量异步落盘**（内存缓冲 + 定时/定量 flush + WAL），避免高频写阻塞网关请求；建库时启用 `PRAGMA journal_mode=WAL`、`PRAGMA busy_timeout=5000` 缓解写锁竞争。

### settings（全局配置 KV）
 | 字段  | 类型   | 说明                                                        |
 | ---   | ---    | ---                                                         |
 | key   | string | 如 `encrypt_seed`、`default_timeout_ms`、`admin_token_hash` |
 | value | string | 值                                                          |

## 6. 路由与故障转移（核心流程）

### 路由选择
1. 网关收到 `chat/completions`，取出 `model` 字段
2. 查 `model_routes[display_name]` 中 `enabled` 且渠道 `health_state=healthy` 的候选
3. 按 `priority` 升序分组；同组内按 `weight` 加权随机选渠道
4. 该渠道内再按 `channel_keys` 的 `weight` 加权随机选一个 Key
5. 无可用候选 → 返回 404 `model_not_found`

### 故障转移
- 触发条件：网络错误、连接超时、上游 5xx、429、以及可解析的错误响应
- 动作：将该渠道标记为失败候选，记录日志，**立即重试下一个候选渠道**，最多 N 次（默认 3）
- 429 附加动作：对应 Key 权重临时降低（惩罚期）
- 连续失败达到阈值（如 5 次）→ 渠道进入 `cooldown`，不再参与路由；由后台探活任务周期性调用该渠道任一模型的轻量请求，成功后自动恢复 `healthy`

### 流式（SSE）处理
- 转发上游 SSE，透传 `text/event-stream` 数据块
- 中途上游断流/报错：无法无缝续传，向客户端发送标准 `event: error` + `message: xxx` 事件后结束；该请求计为失败并覆盖日志
- 首个字节发出后不做渠道切换（避免客户端收到拼接的多路响应）

## 7. API 设计

### 网关 `/{prefix}/v1/*`（prefix 可配，默认 `/`）
 | 方法 | 路径                   | 说明                             |
 | ---  | ---                    | ---                              |
 | POST | `/v1/chat/completions` | 对话补全，支持 `stream=true` SSE |
 | POST | `/v1/completions`      | 文本补全                         |
 | POST | `/v1/embeddings`       | 向量化                           |
 | GET  | `/v1/models`           | 返回所有 enabled 的对外模型名    |

鉴权：`Authorization: Bearer sk-xxx`（对 `users.token_hash` 校验）；额度超额返回 403 + 说明。

### 管理端 `/api/admin/*`
 | 方法    | 路径                           | 说明                                           |
 | ---     | ---                            | ---                                            |
 | CRUD    | `/api/admin/channels`          | 渠道管理，含 `POST /:id/test`（测试连通性）    |
 | CRUD    | `/api/admin/channels/:id/keys` | 渠道 Key 池                                    |
 | CRUD    | `/api/admin/model-routes`      | 模型路由映射                                   |
 | CRUD    | `/api/admin/users`             | 用户、Token 生成/重置、额度设置                |
 | GET     | `/api/admin/logs`              | 请求日志（筛选：时间/模型/渠道/状态）          |
 | GET     | `/api/admin/usage`             | 聚合统计（按天/模型/渠道，调用量/费用/tokens） |
 | GET/PUT | `/api/admin/settings`          | 全局设置：默认超时、加密种子轮换等             |

管理端鉴权：`admin_token`（`.env` 初始注入 / 首次启动自动生成打印）。

## 8. 前端页面

 | 页面     | 内容                                                                                |
 | ---      | ---                                                                                 |
 | 登录     | 输入 admin token（会话存 localStorage，可选 HttpOnly 变体）                         |
 | 仪表盘   | 今日调用量/费用/Token 卡片，近 7/30 天趋势图（ECharts），渠道健康状态，最近失败列表 |
 | 渠道管理 | 渠道表格 + 新增/编辑弹窗（BaseURL、优先级、权重、Key 池批量添加）、测试连接、启停   |
 | 模型路由 | 对外模型名视图，展开显示映射的各渠道与权重，编辑映射                                |
 | 用户管理 | 用户列表，新建/停用、Token 生成（一次性展示）、额度设置与用量查看                   |
 | 请求日志 | 表格 + 时间/模型/状态筛选，点击查看详情（渠道、Key、tokens、耗时、错误）            |
 | 系统设置 | 默认超时、日志保留天数、加密种子轮换                                                |

## 9. 部署（docker-compose）

```yaml
services:
  llmgate:
    build: .
    ports: ["3210:3210"]
    environment:
      - LGM_ADMIN_TOKEN=${LGM_ADMIN_TOKEN}
      - LGM_ENCRYPT_SEED=${LGM_ENCRYPT_SEED}
      - LGM_DB_PATH=/data/llmgate.db
    volumes:
      - ./data:/data
    restart: unless-stopped
```

## 10. 实施路线

### Phase 1 —— MVP（先跑通闭环）
1. 项目骨架：`cmd/server` + `internal/*` 分层 + Vue 工程初始化
2. 建库：内嵌迁移脚本建表 + 首次启动初始化（admin token、默认设置、WAL pragma）
3. adapter/openai：非流式 `chat/completions` 转发
4. router：模型路由选择 + Key 池权重 + 失败重试（非流式）
5. gateway：Bearer 鉴权、`/v1/models`，补齐 `completions` / `embeddings`
6. 管理 API：channels / model-routes / logs / usage / settings
7. 前端：登录 + 仪表盘 + 渠道管理 + 模型路由 + 请求日志
8. Dockerfile + docker-compose，`make dev` / `make build`

### Phase 2 —— 增强
- SSE 流式转发 + 流中断错误事件
- 多用户与额度（quota 累计、403 拦截）
- 健康探活任务 + cooldown 自动恢复
- 429 惩罚机制、Key 池动态权重
- 成本估算细化（每模型单价配置）

### Phase 3 —— 扩展（按需）
- 上游协议适配器扩展（Anthropic / Gemini）
- 请求日志 Prometheus metrics 导出
- 响应缓存（相同请求 hash 命中）
- Webhook/邮件告警（渠道健康、配额告警）

## 11. 约定与说明
- 所有金额字段统一为美元 float（成本估算用）
- 日志保留天数可配置，超期清理（后台定时任务）
- API Key 一律加密存储，接口返回一律掩码（`sk-****ab12`）
- 前端只展示统计与配置，不触碰原始密钥
