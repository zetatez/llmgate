# llmgate Web 前端

llmgate 管理后台前端（英文界面），基于 Vue 3 + TypeScript + Vite + Element Plus + ECharts。

- 页面：Login / Dashboard / Channels / Model Routes / Users / Logs / Settings
- 开发运行：`npm run dev`（Vite 代理 `/api`、`/v1` 到后端 `:8080`，见 `vite.config.ts`）
- 构建：`npm run build` → 产物 `dist/`，由 `scripts/build-web.sh` 拷贝到 `internal/web/dist` 供 Go 二进制嵌入
- 对接后端：`src/api`（axios 封装 + 类型定义），管理 API `/api/admin/*`

> 说明文档以仓库根目录 [README.md](../README.md) 为准。