# Checklist rút gọn — file rủi ro trung bình (🟡)

Các file này conflict về text nhưng không mâu thuẫn logic — union được. Liệt kê ngắn gọn, không cần code trước/sau đầy đủ như nhóm 🔴 vì cách resolve rõ ràng: giữ cả 2 phía, đặt đúng vị trí.

## `internal/router/router.go`
`develop` thêm field `ModelPreferenceHandler`, `MemoryV2Handler` vào `RouterParams` + 1 dòng gọi `RegisterMemoryV2Routes`. `main` thêm nhiều field khác (`SandboxSkillHandler`, `MeEnvVarHandler`, `MCPEndpointHandler`, `MCPServer`, `HostSandbox`...) + route MCP server, sandbox terminal, browser-skill, CORS header mới, middleware `MultipartFormCleanup()`.
- **Resolve**: union tất cả field vào struct `RouterParams` (không field nào trùng tên), union tất cả dòng gọi `Register*Routes`/middleware. Không có logic loại trừ lẫn nhau.
- **Việc cần làm**: sau khi union, chạy `gofmt -w internal/router/router.go` (từng có tiền lệ mất indent ở phiên merge trước — xem `docs/_supermeo/CONVENTIONS.md`), chạy lại `go test ./internal/router/...`.

## `internal/handler/dto/model.go`
`develop` chỉ thêm field `SortOrder int` (2 chỗ: struct `ModelResponse` + gán trong `NewModelResponse`). `main` viết lại phần lớn file: thêm `VendorRef`, che giấu secret trong `ExtraConfig`, field `Capabilities`/`ContextWindow`/`MaxOutputTokens`/`Spec`, viết lại `NewModelResponse`.
- **Resolve**: lấy nền `main`, chèn lại 2 chỗ của `SortOrder` (field trong struct `ModelResponse`, dòng gán trong `NewModelResponse` viết lại của `main`).
- **Việc cần làm**: `go test ./internal/handler/dto/...`, kiểm tra JSON response `/api/v1/models` vẫn có field `sort_order` sau merge.

## `internal/types/custom_agent.go` (phần KHÔNG liên quan `MaxCompletionTokens`)
Ngoài xung đột `MaxCompletionTokens` đã xử lý ở `01-checklist.md` mục #5, `main` còn thêm không liên quan: `BuiltinSkillInstallerID`, `ValidateAvatar()` (giới hạn 64 ký tự avatar), `MaxIterations < 0 → UnlimitedMaxIterations`, đồng bộ `ReasoningEffort` → `Thinking`.
- **Resolve**: giữ nguyên toàn bộ phần thêm của `main` — không liên quan đến thay đổi của `develop`, union tự nhiên.

## `internal/handler/session/qa.go` (phần còn lại ngoài `completeAssistantMessage`)
Đã xử lý điểm chính ở `01-checklist.md` mục #2. Phần diff còn lại của `main` (steering mid-run, `turnLeaseHeld`, `steerSink`, browser-skill fields trong `qaRequestContext`...) không liên quan gì đến thay đổi của `develop` trong file này.
- **Resolve**: lấy nguyên `main` cho toàn bộ phần còn lại, chỉ áp riêng thay đổi đã chốt ở mục #2.

## `config/prompt_templates/*.yaml` (5 file còn lại: `context_template.yaml`, `fallback.yaml`, `intent_prompts.yaml`, `rewrite.yaml`, `generate_summary.yaml`)
`develop` thêm block `vi-VN` vào từng file (cùng điểm chèn i18n). `main` thêm block `ja-JP` (cùng điểm chèn) và ở `fallback.yaml` còn viết lại nội dung `content:` của `standard_fallback_prompt`.
- **Resolve**: union 2 khối locale (`vi-VN` + `ja-JP`) vào cùng 1 danh sách i18n của mỗi file — đặt cạnh nhau theo thứ tự locale hiện có (thường alphabetical hoặc theo thứ tự khai báo `SUPPORTED_LOCALES`). Với `fallback.yaml`: giữ `content:` đã viết lại của `main`, chỉ thêm khối i18n `vi-VN` (không phục hồi `content:` cũ của `develop`).
- **Việc cần làm**: sau merge, chạy test tương ứng (`internal/config/...` nếu có test load prompt template) để xác nhận YAML hợp lệ và mọi locale key cần thiết đều có mặt — đối chiếu với `internal/types/context_helpers.go`'s `LanguageLocaleName` (đã có `vi-VN` từ phiên cô lập trước) để không thiếu key nào tương ứng.

## `config/prompt_templates/agent_system_prompt.yaml`
Rủi ro cao hơn 5 file trên vì `main` viết lại toàn bộ nội dung `content:` của `pure_agent`/`progressive_rag_agent` (không chỉ thêm locale).
- **Resolve**: giữ `content:` đã viết lại của `main`, thêm khối i18n `vi-VN` của `develop` vào đúng danh sách locale (cùng cách làm 5 file trên). Nếu `develop`'s bản `vi-VN` tham chiếu đến đoạn nội dung tiếng Anh cũ mà `main` đã xoá/viết lại, cần đọc lại nội dung mới của `main` và điều chỉnh bản dịch `vi-VN` cho khớp ngữ cảnh mới (không copy máy móc).
- **Việc cần làm**: đọc kỹ nội dung `content:` mới của `main` trước khi merge bản dịch — đây là điểm duy nhất trong nhóm prompt-template cần đọc hiểu thay vì chỉ union.

## `frontend/src/i18n/index.ts`, `frontend/src/i18n/localeKeyAudit.ts`
`develop` thêm `vi-VN`; `main` thêm `ja-JP`. Cùng điểm chèn (import, object entry, mảng thứ tự locale) nhưng không loại trừ nhau.
- **Resolve**: union — cả 2 locale cùng tồn tại.
- **Việc cần làm**: `cd frontend && npx vue-tsc --noEmit`, chạy test `frontend/src/i18n/localeKeyAudit.test.ts` (file mới của `main`) để xác nhận không thiếu key nào giữa các locale.

## `frontend/src/i18n/resolveDefaultLocale.ts` — ⚠️ cần xác nhận riêng
`develop` đổi `BUILT_IN_DEFAULT` từ `'zh-CN'` → `'vi-VN'` (khớp quyết định đã chốt ở phiên cô lập trước — xem `internal/types/context_helpers.go`'s `fallbackDefaultLanguage`). `main` chỉ thêm `'ja-JP'` vào `SUPPORTED_LOCALES`, giữ nguyên default `'zh-CN'`.
- **Resolve**: giữ `BUILT_IN_DEFAULT = 'vi-VN'` của `develop` (đây là quyết định có chủ đích, đã đồng bộ với backend `types.DefaultLanguage()`), union thêm `'ja-JP'` vào mảng `SUPPORTED_LOCALES` của `main`.
- **⚠️ Rủi ro merge ngược**: nếu người resolve không biết bối cảnh, rất dễ vô tình lấy `main`'s `'zh-CN'` default vì nó "trông giống bản gốc/ổn định hơn". Cần ghi rõ trong PR description khi merge thật: "giữ nguyên default `vi-VN`, đây là quyết định có chủ đích của fork, không phải sai sót".

## `frontend/src/i18n/embed.ts`
`develop` thêm object `viEmbedPublish` đầy đủ + đổi default locale trong embed (3 chỗ, cùng logic với `resolveDefaultLocale.ts`). `main` thêm ~1000 dòng nội dung không liên quan (`conversationTime`, `referencesDrawer*`, `artifactDrawer`...) + `ja-JP` + export `EMBED_MESSAGES`.
- **Resolve**: union nội dung message-key mới của `main` (không đụng), union `viEmbedPublish` + `ja-JP`, giữ default `vi-VN` của `develop` ở cả 3 chỗ (cùng lưu ý ⚠️ như trên).
- **Việc cần làm**: test riêng embed bundle (`frontend/src/i18n/embedLocale.test.ts`, file mới của `main`) sau merge.

## `cli/internal/mcp/server.go`
`develop`: sửa comment số lượng tool + thêm field `memoryService` vào interface `ServiceClient`. `main`: thêm import `sdk "github.com/Tencent/WeKnora/client"` + static assertion `var _ ServiceClient = (*sdk.Client)(nil)`.
- **Resolve**: union cả 2 — giữ field `memoryService` của develop, giữ static assertion của main, cập nhật lại số lượng tool trong comment cho khớp con số thật hiện tại (đếm lại, không giữ nguyên số cũ của bên nào).
- **Việc cần làm**: `go build ./cli/...`, xác nhận static assertion `var _ ServiceClient = (*sdk.Client)(nil)` vẫn pass compile (tức `sdk.Client` có implement đủ field/method mới bao gồm `memoryService` nếu assertion yêu cầu — kiểm tra kỹ vì đây là compile-time check, lỗi sẽ chặn build ngay).
