# llmgate

A multi-user, OpenAI-compatible gateway that routes each user's traffic through the upstream API keys they contributed. Go + SQLite, single container, embedded admin UI.

```mermaid
flowchart LR
    CLI(["Client · user A"])
    CLI2(["Client · user B"])

    subgraph GW["llmgate"]
        direction LR
        A(["auth"]) --> Q(["quota"]) --> R(["routing"]) --> F(["failover"])
    end

    CLI -- "OpenAI · SSE" --> A
    CLI2 -- "OpenAI · SSE" --> A
    R -- "user A's keys" --> P1[("Plan A · own key pool")]
    F -- "user B's keys" --> P2[("Plan B · own key pool")]
    F -. "fallback" .-> P3[("other plans")]

    style CLI fill:#fff7ed,stroke:#ea580c,stroke-width:2px
    style CLI2 fill:#fff7ed,stroke:#ea580c,stroke-width:2px
    style A fill:#e0f2fe,stroke:#0284c7
    style Q fill:#e0f2fe,stroke:#0284c7
    style R fill:#e0f2fe,stroke:#0284c7
    style F fill:#e0f2fe,stroke:#0284c7
    style P1 fill:#f0fdf4,stroke:#16a34a
    style P2 fill:#f0fdf4,stroke:#16a34a
    style P3 fill:#f0fdf4,stroke:#16a34a
```

## Features

- Multi-user: the admin creates accounts; users sign in with username and password and manage only their own data.
- Bring your own key (BYOK): users record their own upstream API keys into admin-defined channels. Every key is tagged with its owner, is visible only to that owner, and is used only for that owner's requests.
- Per-user gateway tokens: users issue and revoke multiple `sk-` tokens for their own consumption; each token resolves to its owner.
- Channel key pools with priority / weight based routing; downstream switch on rate limit, quota exhaustion, or upstream error.
- Circuit breaker keyed to a consecutive-failure threshold (default 6 failures in 30s) with exponential backoff, cleared automatically after a successful probe.
- Per-user quotas; API keys and gateway tokens stored AES-256-GCM encrypted; disabling a channel deactivates all of its keys and routes.
- Per-user, per-model usage statistics and a dashboard; admin UI covering channels, routes, users, logs, and settings.

## Deploy

```bash
cp .env.example .env           # set LGM_ADMIN_TOKEN
docker compose up -d --build   # open http://localhost:3210
```

Setup: sign in with the admin token, create a channel and (optionally) test it, then create user accounts under **Users**. A user then signs in with their username and password, adds their own API keys under **My API Keys**, and issues a gateway token under **My Tokens**.

```bash
curl http://localhost:3210/v1/chat/completions \
  -H "Authorization: Bearer sk-your-token" -H "Content-Type: application/json" \
  -d '{"model":"deepseek/deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}'
```

## Configuration

Main environment variables are documented in [`.env.example`](.env.example) (`LGM_ADMIN_TOKEN`, `LGM_HTTP_ADDR`, `LGM_PROXY`, ...). Circuit-breaker parameters can be tuned in the admin Settings.

## Docs

- Deployment & operations: [deploy.md](docs/deploy.md)
- Design: [design.md](docs/design.md)

## License

[MIT](LICENSE) © 2026 Ma Tianyi