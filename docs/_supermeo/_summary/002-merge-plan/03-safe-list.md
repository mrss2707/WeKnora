# Danh sách xác nhận an toàn — không cần resolve tay

Các vùng dưới đây đã được xác minh bằng `git merge-tree --write-tree develop main` (dry-run thật) và/hoặc đối chiếu diff trực tiếp. Không xuất hiện trong danh sách `CONFLICT` — Git tự merge sạch, hoặc hai bên không chạm cùng vùng. Liệt kê ra để không tốn công kiểm tra lại khi thực hiện merge thật.

## Migration & DI — cô lập module hoạt động đúng như thiết kế

- `internal/database/migration.go`, `internal/database/migration_source.go` — **core composite migration logic hoàn toàn sạch**, `main` không đụng tới. Dải reserved `900000-909999` cho module vẫn an toàn tuyệt đối: `main`'s core migrations hiện tiến tới `000109` (từ `000084` tại merge-base), vẫn nằm dưới dải reserved.
- `internal/container/container.go` — `main` thêm ~260 dòng (chủ yếu additive: `startTenantSkillReaper`, `startForkSnapshotReaper`, các provider MCP endpoint/sandbox skill/env-var/tenant-skill mới) nhưng **tự động merge sạch**. Xác nhận pattern "1 điểm chèn, 1 dòng gọi" (`registerMemoryV2`, `registerCrossSessionMemory`) đã bảo vệ file này khỏi conflict dù `main` mở rộng đáng kể.
- `migrations/versioned/` trên `main` — không có file nào rơi vào dải `900000-909999`, xác nhận qua kiểm tra trực tiếp toàn bộ tên file.

## Backend — không bị `main` đụng tới trong vùng develop đã sửa

- `internal/application/service/chat_pipeline/query_expansion.go` — `main` không sửa file này.
- `internal/application/service/metric/common.go` — `main` không sửa file này.
- `internal/infrastructure/chunker/patterns.go`, `tokens.go`, `profiler.go` — `main` chỉ sửa `splitter.go` và `strategy.go` trong cùng thư mục (không liên quan langdata registry của develop). `main`'s thay đổi trong chunker (`fix(chunking): 修复内联 HTML 表格与保护模式导致的超长 chunk`) không giao nhau với vùng ngôn ngữ mà develop mở rộng.
- `internal/application/repository/retriever/qdrant/repository.go` — mặc dù `main` sửa 223 dòng trong file này, vẫn **auto-merge sạch** (không nằm trong danh sách CONFLICT).
- `internal/config/config.go` — `main` sửa 63 dòng, auto-merge sạch (khác với `config/config.yaml` — file YAML — có conflict giá trị, xem `01-checklist.md` mục #7).
- `internal/infrastructure/web_fetch/fetcher.go` — auto-merge sạch. Xác nhận `types.AcceptLanguageHeader(ctx)` (abstraction đã dùng ở đây từ phiên cô lập trước) không bị `main` động tới.

## Router — chỉ 1 file thật sự conflict trong nhóm router

- `internal/router/router_api_key_capabilities_test.go`, `internal/router/routes_infra.go` — auto-merge sạch dù `develop` có sửa (`routes_infra.go` thêm route `model preferences`). Chỉ `internal/router/router.go` conflict (xem `02-checklist-medium.md`).

## Frontend — union tự nhiên, không cần resolve đặc biệt

- `frontend/src/views/agent/AgentList.vue`, `frontend/src/views/dev/MarkdownTestPage.vue`, `frontend/src/components/AgentEmbedChannelPanel.vue`, `frontend/src/i18n/locales/workspaceTerminology.test.ts` — auto-merge sạch dù có thay đổi ở cả 2 bên.
- `frontend/package.json`, `frontend/package-lock.json` — auto-merge sạch.

## Không có trên `main` nữa — cần lưu ý riêng khi merge (đã ghi trong `01-checklist.md`)

Hai file bị `main` xoá (modify/delete conflict), `develop` vẫn còn sửa đổi trên chúng:
- `internal/models/chat/anthropic.go` — xem `01-checklist.md` mục #3.
- `skills/preloaded/data-processor/scripts/extract_info.py` — xác nhận `CONFLICT (modify/delete)`: `main` đã xoá toàn bộ preloaded skills theo host (`feat(agent): unify sandbox tools and drop host preloaded skills`, PR #3033, commit `d5cb8425`), trong khi `develop` vẫn sửa đổi file này. **Khuyến nghị**: xác nhận tính năng "host preloaded skills" có còn cần thiết trên `develop` không trước khi quyết định giữ hay theo `main` xoá hẳn — đây là quyết định sản phẩm, không phải kỹ thuật thuần. Nếu `main`'s lý do xoá (chuyển sang unified sandbox tools) vẫn hợp lý cho `develop`, nên theo `main` xoá file; nếu `develop` còn phụ thuộc vào cơ chế preloaded-skill cũ, cần đánh giá riêng trước khi merge.

## `client/memory.go` — add/add conflict (2 tính năng trùng tên file, khác nội dung)

`develop` tạo file này cho Memory V2 REST client SDK (`AgentMemory`, endpoint `/api/v1/memories`). `main` tạo file cùng tên cho tính năng "cross-session memory" hoàn toàn khác (`MemorySettings`, `workspace_enabled`/`personal`). Đây **không phải conflict nội dung cùng 1 tính năng** — là 2 tính năng trùng tên file ngẫu nhiên.
- **Resolve**: đổi tên 1 trong 2 file để tránh mất tính năng nào (ví dụ giữ `client/memory.go` cho tính năng của `main` — vì đây là client SDK công khai, đổi tên sau khi đã publish sẽ phá vỡ người dùng SDK — và đặt Memory V2 client vào `client/memory_v2.go`, khớp đúng quy ước đặt tên `_v2` đã dùng nhất quán ở phía backend (`internal/application/service/memory_v2/`, `internal/router/memory_v2.go`, ...)).
- **Việc cần làm**: đổi tên `client/memory.go` (bản develop) → `client/memory_v2.go` trước khi resolve merge, cập nhật mọi import/reference trong `cli/` nếu có.
