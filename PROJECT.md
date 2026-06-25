# WeKnora v0.6.2 — LLM-Powered Knowledge Management Framework

MIT-licensed, open-source by Tencent. Turns documents into living knowledge via RAG + Agent + Auto-Wiki. Self-hosted, multi-tenant.

## Architecture

```
cmd/server (Go/Gin, port 8080) ── REST /api/v1
  ├── internal/agent/          ReAct Agent engine, 30+ tools, skills, MCP, memory
  ├── internal/application/    Business logic (RAG pipeline, wiki, knowledge, chat)
  ├── internal/handler/        HTTP handlers + SSE streaming + IM webhooks
  ├── internal/models/         LLM abstraction (chat, embedding, rerank, VLM, ASR)
  ├── internal/infrastructure/ Doc chunker, parser (gRPC→Python), web search, langdata
  ├── internal/mcp/            MCP client/manager/OAuth
  ├── internal/im/             7 IM channels (WeCom, Feishu, Slack, Telegram, DingTalk, Mattermost, WeChat)
  └── internal/middleware/     JWT auth, RBAC (4-tier), rate-limit, audit

docreader/                     Python gRPC doc parser (10+ formats)
frontend/                      Vue 3 + TypeScript + Vite + TDesign + Pinia + vue-i18n (5 locales)
cli/                           Go CLI (weknora), MCP server mode, v0.9
mcp-server/                    Python MCP server
```

## Tech Stack

| Layer | Primary |
|-------|---------|
| Backend | Go 1.26, Gin, GORM, pgx, asynq (Redis), uber-go/dig (DI), swaggo/swagger |
| DB | PostgreSQL (primary) / SQLite (Lite mode) / MySQL (Doris) |
| Vector DB | pgvector, Elasticsearch, OpenSearch, Milvus, Weaviate, Qdrant, Doris, TencentVectorDB |
| Cache/Queue | Redis / in-memory (Lite mode) |
| Frontend | Vue 3 + TS + Vite + TDesign + Pinia |
| Desktop | Wails v2 (Go + WebView2) |
| LLMs | 25+ providers (OpenAI, Anthropic, DeepSeek, Qwen, Zhipu, Ollama, etc.) |
| Embedding | Ollama, BGE, GTE, Zhipu, Jina, NVIDIA, Gemini, OpenAI-compatible |
| Object Storage | Local, MinIO, S3, TOS, OSS, KS3, OBS |
| Tracing | Langfuse |
| KG (optional) | Neo4j |
| Doc parsing | Python gRPC service (golang-migrate/migrate, DuckDB, chromedp) |

## Key Features

- **RAG Pipeline** — multi-stage: query expansion → parallel vector/BM25/hybrid search → rerank → merge → wiki boost
- **ReAct Agent** — progressive reasoning, 30+ tools, skills sandbox (Docker/local), MCP integration, human-in-the-loop
- **Wiki Mode** — agent-driven auto-generated Markdown wiki with knowledge graph, hierarchy, ingestion pipeline
- **Multi-Tenant RBAC** — Owner/Admin/Contributor/Viewer per tenant, per-KB ownership, audit log
- **7 IM Channels** — WeCom, Feishu, Slack, Telegram, DingTalk, Mattermost, WeChat
- **Lite Mode** — single binary: SQLite + in-memory queue + sqlite-vec + local storage
- **10+ doc formats** — PDF, Word, TXT, MD, HTML, Images, CSV, Excel, PPT, JSON
- **KB types** — FAQ / Document / Wiki, multi-source ingestion (Feishu, Notion, Yuque)
- **i18n** — 5 locales: zh-CN, en-US, ko-KR, vi-VN, ru-RU

## Current Progress (2026-06-25)

- **Version**: 0.6.2, 62 DB migrations
- **Active work**: Vietnamese (vi-VN) i18n across full stack — chunker, tokenizer, metrics, prompts, agent tools, locale files. All 5 locales complete (5800+ lines each).
- **Recent**: MCP OAuth support (migration 000062), wiki page hierarchy (000061)
- **Testing**: 379 Go test files, 104 frontend test files, acceptance/e2e suite
- **Ship**: Docker images (wechatopenai/weknora-*), Helm charts, pre-built CLI binaries
