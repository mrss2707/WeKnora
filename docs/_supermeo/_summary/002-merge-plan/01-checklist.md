# Checklist chi tiết — 8 file rủi ro cao (🔴)

Mỗi mục: **Xung đột** (nguyên nhân) → **Code develop** / **Code main** (trước) → **Phương án merge** (sau, cụ thể) → **Vì sao cả 2 logic đều sống sót**.

---

## 1. `internal/application/repository/message.go` — `UpdateMessage`

### Xung đột
`develop` chỉ sửa 1 dòng: thêm `.Select("*")`. `main` viết lại toàn hàm thành transaction + gọi `writeMessageArtifacts`. Đây là **conflict vị trí trùng dòng nhưng cả 2 thay đổi đều cần thiết và độc lập** — không phải chọn 1 bên.

### Code develop (trước)
```go
// UpdateMessage updates an existing message
func (r *messageRepository) UpdateMessage(ctx context.Context, message *types.Message) error {
	return r.db.WithContext(ctx).Model(&types.Message{}).Where(
		"id = ? AND session_id = ?", message.ID, message.SessionID,
	).Select("*").Updates(message).Error
}
```
Lý do có `.Select("*")`: commit `8b97f164` ("fix: empty chat response, output truncation, and i18n modal crash") — không có nó, GORM `Updates()` bỏ qua mọi field có zero-value (chuỗi rỗng, `0`, `false`), khiến các field đã được set về rỗng một cách hợp lệ (ví dụ nội dung streaming bị truncate rồi ghi đè bằng chuỗi rỗng) không bao giờ được lưu xuống DB.

### Code main (trước)
```go
// UpdateMessage updates an existing message. Artifacts are rewritten only
// when message.Artifacts is non-nil (see writeMessageArtifacts).
func (r *messageRepository) UpdateMessage(ctx context.Context, message *types.Message) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&types.Message{}).Where(
			"id = ? AND session_id = ?", message.ID, message.SessionID,
		).Updates(message).Error; err != nil {
			return err
		}
		return writeMessageArtifacts(tx, message)
	})
}
```
`writeMessageArtifacts` (file riêng `message_artifact.go`, main-only) đồng bộ bảng `message_artifacts` — xoá rồi ghi lại theo `message.Artifacts`, giữ lại tombstone (`DeletedAt`) của các artifact đã xoá trước đó để tránh "hồi sinh" bản ghi đã bị xoá dữ liệu vật lý. **Quan trọng: `main` vẫn dùng `.Updates(message)` (không `.Select("*")`) — bug gốc develop đã fix vẫn còn nguyên trên main.**

### ⚠️ Phương án merge (bắt buộc — giữ cả 2 fix)
Lấy cấu trúc transaction của `main`, chèn `.Select("*")` vào đúng chỗ:

```go
// UpdateMessage updates an existing message. Artifacts are rewritten only
// when message.Artifacts is non-nil (see writeMessageArtifacts).
func (r *messageRepository) UpdateMessage(ctx context.Context, message *types.Message) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&types.Message{}).Where(
			"id = ? AND session_id = ?", message.ID, message.SessionID,
		).Select("*").Updates(message).Error; err != nil {
			return err
		}
		return writeMessageArtifacts(tx, message)
	})
}
```

**Cân nhắc phụ**: `.Select("*")` buộc GORM ghi đè TẤT CẢ cột, kể cả cột `main` mới thêm mà `message` struct không set tường minh (ví dụ `SandboxCheckpoint`, `ContextCheckpoint`, `Usage` — xem field mới trong `versionedSQLiteColumns` ở mục #6). Cần kiểm tra: nếu `UpdateMessage` được gọi ở nơi chỉ set 1-2 field (ví dụ chỉ cập nhật `IsCompleted`), `.Select("*")` sẽ ghi đè các field khác về zero-value nếu struct truyền vào không mang giá trị hiện tại của chúng. Kiểm tra từng call site của `UpdateMessage` trên `main` (đặc biệt trong `qa.go`, xem mục #2) xem `assistantMessage` truyền vào có đầy đủ field hay không trước khi merge — nếu có call site chỉ set 1 field, cân nhắc dùng `.Select(fieldList...)` thay vì `.Select("*")` tại đúng call site đó, hoặc đảm bảo struct luôn được load đầy đủ trước khi gọi.

### Việc cần làm
- [ ] Áp code merge ở trên vào `message.go`.
- [ ] Grep toàn repo `UpdateMessage(ctx` để liệt kê mọi call site trên `main`, xác nhận struct `*types.Message` truyền vào luôn đầy đủ field (không phải struct rỗng chỉ set 1-2 field).
- [ ] Test thủ công: gửi 1 tin nhắn, để LLM trả lời rỗng/bị cắt giữa chừng (ngắt kết nối sớm), xác nhận DB không giữ nội dung cũ (test case gốc của commit `8b97f164`).
- [ ] Chạy `go test ./internal/application/repository/...`.

---

## 2. `internal/handler/session/qa.go` — `completeAssistantMessage`

### Xung đột
`develop` sửa nhỏ: bọc `context.WithoutCancel(ctx)` quanh lời gọi `UpdateMessage`. `main` refactor lớn: đổi chữ ký hàm trả về `error`, tách ra 2 hàm gọi mới (`completeQuickAnswerTurn`, `completeStreamAssistantMessage`), và tự áp `context.WithoutCancel` từ phía caller thay vì bên trong hàm.

### Code develop (trước)
```go
// completeAssistantMessage marks an assistant message as complete, updates it,
// and asynchronously indexes the Q&A pair into the chat history knowledge base.
func (h *Handler) completeAssistantMessage(
	ctx context.Context, assistantMessage *types.Message, userQuery, userMessageID string,
) {
	assistantMessage.UpdatedAt = time.Now()
	assistantMessage.IsCompleted = true
	// Use WithoutCancel so the DB write survives client disconnect (SSE stream
	// may end before the LLM finishes generating). Otherwise a cancelled context
	// can cause GORM to skip the UPDATE silently.
	_ = h.messageService.UpdateMessage(context.WithoutCancel(ctx), assistantMessage)

	bgCtx := context.WithoutCancel(ctx)
	go h.messageService.IndexMessageToKB(bgCtx, userQuery, assistantMessage.Content, assistantMessage.ID, assistantMessage.SessionID)
	if userQuery != "" && h.suggestionService != nil {
		go func() {
			if _, err := h.suggestionService.EnsureFollowUps(
				bgCtx, assistantMessage.SessionID, assistantMessage.ID, false,
			); err != nil {
				logger.Warnf(bgCtx, "follow-up suggestion generation failed for message %s: %v", assistantMessage.ID, err)
			}
		}()
	}
	if userQuery != "" {
		go h.recordTurnMemory(bgCtx, assistantMessage, userQuery, userMessageID)
	}
}
```

### Code main (trước)
```go
// completeAssistantMessage marks an assistant message as complete, updates it,
// and asynchronously indexes the Q&A pair into the chat history knowledge base.
func (h *Handler) completeAssistantMessage(
	ctx context.Context, assistantMessage *types.Message, userQuery, userMessageID string,
) error {
	assistantMessage.UpdatedAt = time.Now()
	assistantMessage.IsCompleted = true
	if err := h.messageService.UpdateMessage(ctx, assistantMessage); err != nil {
		logger.Errorf(ctx, "Failed to persist assistant message %s: %v", assistantMessage.ID, err)
		return err
	}

	bgCtx := context.WithoutCancel(ctx)
	if indexCtx, ok := h.sessionTenantInfoContext(bgCtx); ok {
		go h.messageService.IndexMessageToKB(
			indexCtx, userQuery, assistantMessage.Content, assistantMessage.ID, assistantMessage.SessionID)
	}
	if userQuery != "" && h.suggestionService != nil {
		go func() {
			if _, err := h.suggestionService.EnsureFollowUps(
				bgCtx, assistantMessage.SessionID, assistantMessage.ID, false,
			); err != nil {
				logger.Warnf(bgCtx, "follow-up suggestion generation failed for message %s: %v", assistantMessage.ID, err)
			}
		}()
	}
	if userQuery != "" {
		go h.recordTurnMemory(bgCtx, assistantMessage, userQuery, userMessageID)
	}
	return nil
}
```
Caller `completeQuickAnswerTurn` (main) đã tự đặt `ctx = context.WithoutCancel(ctx)` **trước khi** gọi `completeStreamAssistantMessage` → `completeAssistantMessage`. Nghĩa là `UpdateMessage(ctx, ...)` bên trong `completeAssistantMessage` **đã nhận `ctx` không-thể-cancel** — hiệu ứng giống hệt develop, chỉ khác chỗ áp dụng (caller vs callee).

### Phương án merge
Lấy nguyên chữ ký + logic mới của `main` (trả `error`, propagate lỗi thay vì nuốt bằng `_ =`, có `sessionTenantInfoContext` gating). **Không cần thêm gì** — thuộc tính "sống sót khi client disconnect" của develop đã được `main` bảo toàn qua đường khác (caller wrap thay vì callee wrap), nên đây thực chất **không phải xung đột logic thật, chỉ là xung đột text do đổi chữ ký hàm**.

Việc duy nhất cần xác minh: đảm bảo **mọi** call site cũ của `completeAssistantMessage` trên `develop` (nếu có nơi khác ngoài `completeQuickAnswerTurn`/`completeStreamAssistantMessage` gọi trực tiếp) đều được cập nhật để xử lý giá trị trả về `error` mới, và được gọi với `ctx` đã qua `context.WithoutCancel` từ phía caller (theo đúng pattern main) — không phải mọi nơi tự thêm `WithoutCancel` lần nữa bên trong hàm (double-wrap vô hại nhưng thừa).

### Việc cần làm
- [ ] Lấy nguyên bản `main` cho `completeAssistantMessage`, `completeQuickAnswerTurn`, `completeStreamAssistantMessage`, `sessionTenantInfoContext`.
- [ ] Grep `completeAssistantMessage(` trong toàn bộ `qa.go` (cả 2 nhánh) để đối chiếu danh sách call site — xác nhận không có call site nào của `develop` bị bỏ sót (ví dụ nếu `develop` đã thêm 1 luồng gọi riêng cho tính năng agent nào đó chưa có trên `main`).
- [ ] Test: ngắt kết nối SSE giữa chừng khi model đang stream, xác nhận message vẫn được lưu đầy đủ vào DB (test case gốc của develop's `WithoutCancel` fix) — cùng lúc verify với test case #1 (`.Select("*")`).
- [ ] Chạy `go build ./...` để bắt các call site chưa cập nhật chữ ký `error`.

---

## 3. `internal/models/chat/anthropic.go` — MaxTokens default (modify/delete)

### Xung đột
`main` **xoá hẳn file này** — toàn bộ kiến trúc `internal/models/chat/*` (provider theo file) được thay bằng `internal/models/api/<protocol>/` + `internal/models/catalog/` + `internal/models/vendors/`. `develop` chỉ sửa 1 dòng trong file này (`MaxTokens: 1024 → 32768`). Không thể giữ file — phải port giá trị sang kiến trúc mới.

### Code develop (trước, file sẽ mất)
```go
func (c *AnthropicChat) buildRequest(messages []Message, opts *ChatOptions) anthropicRequest {
	req := anthropicRequest{
		Model:     c.modelName,
		MaxTokens: 32768,
		Messages:  make([]anthropicMessage, 0, len(messages)),
	}
	...
```

### Code main (trước, nơi cần port giá trị tới)
`internal/models/api/anthropicmessages/request.go`:
```go
maxTokens := opts.CompletionBudget()
if maxTokens <= 0 {
	maxTokens = s.DefaultMaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
}
```
`internal/models/catalog/compat.go`:
```go
func DefaultAnthropicMessages() AnthropicMessagesSettings {
	return AnthropicMessagesSettings{
		...
		DefaultMaxTokens:           4096,
		...
```

### Phương án merge
1. **Chấp nhận xoá file** `internal/models/chat/anthropic.go` (theo `main`) — giữ file cũ sẽ làm sống lại kiến trúc kép, không tương thích với hệ catalog/vendor mới.
2. Đổi `DefaultMaxTokens: 4096` → `DefaultMaxTokens: 32768` trong `DefaultAnthropicMessages()` (`internal/models/catalog/compat.go`) để giữ nguyên **ý định** của develop (Anthropic model cần completion budget lớn hơn mức 4096 mặc định của main).
3. Cân nhắc: giá trị này áp dụng cho **mọi** vendor dùng `DefaultAnthropicMessages()` như baseline (không chỉ 1 model) — xác nhận với business owner rằng 32768 là giá trị mong muốn ở tầm catalog-wide, không phải riêng 1 model. Nếu chỉ muốn override cho 1 vendor cụ thể, dùng field overlay `AnthropicMessagesCompat.DefaultMaxTokens *int` (per-vendor override) thay vì đổi baseline dùng chung.

### Việc cần làm
- [ ] Xác nhận với người quyết định: 32768 là default **toàn hệ Anthropic Messages** hay chỉ 1 vendor? (ảnh hưởng chọn sửa `DefaultAnthropicMessages()` baseline hay override per-vendor).
- [ ] Sửa `internal/models/catalog/compat.go` theo quyết định trên.
- [ ] Chạy test `internal/models/api/anthropicmessages/...` và `internal/models/catalog/...` để xác nhận default mới không phá test có sẵn của `main` đang assert `4096`.
- [ ] Xoá `internal/models/chat/anthropic.go` khỏi tree sau merge (đồng ý theo `main`).

---

## 4. `internal/models/embedding/embedder.go` — case `"generic"`

### Xung đột
`develop` thêm 1 dòng case `"generic"` vào switch `newEmbedder`. `main` xoá toàn bộ switch theo provider (~160 dòng), thay bằng `newRemoteEmbedder(config, pooler)` — 1 hàm duy nhất dùng `catalog.Resolve()` để tra cứu vendor. `"generic"` trên `main` không còn là `types.ModelSource`, mà là **vendor ID** (`catalog.GenericID = "generic"`) nằm trong tầng catalog hoàn toàn khác.

### Code develop (trước)
```go
func newEmbedder(config Config, pooler EmbedderPooler, ollamaService *ollama.OllamaService) (Embedder, error) {
	switch strings.ToLower(string(config.Source)) {
	case string(types.ModelSourceLocal):
		return NewOllamaEmbedder(...)
	case "generic", string(types.ModelSourceRemote):
		// ... routing logic cũ (per-provider switch dài ~160 dòng)
```

### Code main (trước)
```go
func newEmbedder(config Config, pooler EmbedderPooler, ollamaService *ollama.OllamaService) (Embedder, error) {
	switch strings.ToLower(string(config.Source)) {
	case string(types.ModelSourceLocal):
		return NewOllamaEmbedder(config.BaseURL,
			config.ModelName, config.TruncatePromptTokens, config.Dimensions, config.ModelID, pooler, ollamaService)
	case string(types.ModelSourceRemote):
		return newRemoteEmbedder(config, pooler)
	default:
		return nil, fmt.Errorf("unsupported embedder source: %s", config.Source)
	}
}
```
`newRemoteEmbedder` gọi `catalog.Resolve(catalog.Ref{Provider: config.Provider, ...})` — nếu `config.Provider` là chuỗi rỗng hoặc không khớp vendor nào đã đăng ký, catalog tự fallback về `catalog.GenericID` (vendor OpenAI-compatible chung) bên trong `catalog.Resolve`, **không cần** case riêng ở tầng `embedder.go`.

### Phương án merge
1. **Không port case `"generic"` theo cách cũ** — không có chỗ để chèn, và kiến trúc catalog mới đã tự xử lý "generic" đúng ngữ nghĩa (fallback vendor) ở tầng thấp hơn.
2. Lấy nguyên bản `newEmbedder`/`newRemoteEmbedder` của `main`.
3. Kiểm tra thực tế: nếu trước đây `develop` cần case `"generic"` vì có nơi trong code (hoặc dữ liệu DB cũ) set `Model.Source = "generic"` trực tiếp làm giá trị `types.ModelSource`, cần xác nhận `catalog.Resolve()` trên `main` xử lý đúng khi `config.Source` (không phải `config.Provider`) mang giá trị `"generic"` — vì `newEmbedder`'s switch trên `main` chỉ nhận `ModelSourceLocal`/`ModelSourceRemote`, giá trị `Source = "generic"` sẽ rơi vào nhánh `default: return nil, fmt.Errorf("unsupported embedder source: %s", ...)` và **lỗi runtime**.

### Việc cần làm
- [ ] Grep toàn repo (cả DB migration/seed data nếu có) tìm nơi nào set `Model.Source` (không phải `Provider`) bằng chuỗi `"generic"` literal.
- [ ] Nếu có: cần thêm bước data-migration đổi `Source: "generic"` → `Source: "remote"` (kèm `Provider` phù hợp) trước khi merge production, HOẶC thêm normalize tại tầng đọc DB.
- [ ] Nếu không có (rất có thể — `"generic"` trên develop nhiều khả năng chỉ là alias phòng hờ chưa từng dùng thật): chỉ cần lấy nguyên `main`, không cần thêm gì.
- [ ] Chạy test `internal/models/embedding/...`.

---

## 5. `internal/types/custom_agent.go` — `MaxCompletionTokens` default (⚠️ xung đột mô hình thiết kế)

### Xung đột
Đây là **xung đột ngữ nghĩa thật**, không chỉ giá trị số. `develop`: materialize default `32768` ngay lúc `EnsureDefaults()` chạy (thường tại thời điểm lưu config). `main`: cố tình **không** materialize — để `0` nghĩa là "chưa cấu hình, tính default theo ngữ cảnh lúc gọi" qua `types.AgentRoundMaxCompletionTokensFor(configured, sandboxConfigID)`, với các mức khác nhau: quick-answer `2048`, smart-reasoning không sandbox `4096`, smart-reasoning có sandbox `24576`.

### Code develop (trước)
```go
if a.Config.MaxCompletionTokens == 0 {
	a.Config.MaxCompletionTokens = 32768
}
```

### Code main (trước)
```go
// MaxCompletionTokens 0 means "use DefaultMaxCompletionTokens at call
// time". Do not materialize a number here — that would make the editor
// treat a chosen default as a custom cap.
```
(không còn khối `if == 0` nào — cố ý bỏ)

Hàm phân giải mới (`internal/types/agent.go`):
```go
const DefaultSmartReasoningMaxCompletionTokens = 4096
const DefaultAgentMaxCompletionTokens = 24576      // khi agent có quyền ghi sandbox file
const DefaultQuickAnswerMaxCompletionTokens = 2048

func DefaultMaxCompletionTokens(agentMode, sandboxConfigID string) int { ... }
func AgentRoundMaxCompletionTokensFor(configured int, sandboxConfigID string) int {
	if configured > 0 {
		if NeedsSandboxWriteCompletionBudget(sandboxConfigID) && configured < MinSandboxWriteCompletionTokens {
			return MinSandboxWriteCompletionTokens // floor 8192
		}
		return configured
	}
	return DefaultMaxCompletionTokens(AgentModeSmartReasoning, sandboxConfigID)
}
```
Test có sẵn trên `main` khẳng định thiết kế này: `TestEnsureDefaults_MaxCompletionTokensByMode` (`internal/types/custom_agent_test.go:47-69`) — assert giá trị **vẫn là `0`** sau `EnsureDefaults()` cho cả quick-answer lẫn smart-reasoning, và giá trị đã set tường minh (ví dụ `64000`) được giữ nguyên không bị ghi đè.

### ⚠️ Phương án merge — KHÔNG giữ khối `if == 0 { = 32768 }` của develop
Đây là trường hợp **bắt buộc phải bỏ code của `develop`**, không phải "hợp nhất cả 2":
1. Lấy nguyên bản `EnsureDefaults()` của `main` (bỏ khối materialize-default).
2. Ý định thật sự của develop (agent cần completion budget lớn hơn mặc định 4096) được chuyển thành: sửa `DefaultSmartReasoningMaxCompletionTokens` (hoặc `DefaultAgentMaxCompletionTokens` nếu ý là cho agent có sandbox) trong `internal/types/agent.go` — **cùng cách tiếp cận với mục #3** (sửa baseline default, không hard-code lại tại điểm gọi).
3. Nếu ý định develop là *toàn bộ* mọi agent (kể cả quick-answer) đều cần completion budget 32768, cần cân nhắc kỹ — vì thiết kế mới của main cố tình phân tầng theo agent mode + sandbox để tránh lãng phí token cho các luồng nhẹ (quick-answer). Nên hỏi lại: nếu chỉ 1-2 agent cụ thể cần 32768, dùng field `MaxCompletionTokens` tường minh trên agent đó (giá trị > 0 luôn được tôn trọng qua `AgentRoundMaxCompletionTokensFor`) thay vì đổi default toàn hệ thống.

### Việc cần làm
- [ ] Xác nhận với người quyết định: ý định gốc của việc bump `2048 → 32768` trên develop là gì — áp dụng cho mọi agent, hay chỉ một nhóm agent cụ thể cần completion dài? (quyết định này chi phối sửa ở đâu: baseline `internal/types/agent.go`, hay set tường minh trên từng agent).
- [ ] Lấy nguyên `EnsureDefaults()` của `main`, **không** mang theo khối materialize của develop.
- [ ] Áp giá trị mới (nếu cần) vào đúng hằng số trong `internal/types/agent.go`.
- [ ] Chạy `go test ./internal/types/... -run TestEnsureDefaults` — đặc biệt `TestEnsureDefaults_MaxCompletionTokensByMode` phải pass nguyên vẹn.

---

## 6. `internal/database/migration_sqlite_versioned_schema_test.go`

### Xung đột
Không phải xung đột logic — là **develop đứng sau main quá xa** trên cùng 1 cơ chế test (kiểm tra schema SQLite khớp với migration PostgreSQL). `develop`: 268 dòng, version kỳ vọng `11`, 5 bảng theo dõi. `main`: 386 dòng, version kỳ vọng `29`, 16 bảng theo dõi (bao gồm toàn bộ bảng mới: `mcp_endpoints`, `tenant_skills`, `message_artifacts`, `memory_extraction_sessions`, ...), cơ chế copy migration cũng được tổng quát hoá (`copySQLiteMigrationsV4` → `copySQLiteMigrationsThrough(t, repoRoot, maxVersion)` đọc động thư mục thay vì hardcode danh sách).

### Code develop (trước — phần khác biệt chính)
```go
var versionedSQLiteTables = []string{
	"task_pending_ops",
	"task_dead_letters",
	"system_settings",
	"knowledge_processing_spans",
	"knowledge_tag_relations",
}
// expectedSQLiteMigrationVersion = 11
```

### Code main (trước — superset)
```go
var versionedSQLiteTables = []string{
	"memory_extraction_sessions",
	"task_pending_ops",
	"task_dead_letters",
	"system_settings",
	"knowledge_processing_spans",
	"knowledge_tag_relations",
	"browser_devices",
	"browser_pairings",
	"browser_task_interruptions",
	"fork_snapshot_leases",
	"mcp_endpoints",
	"message_artifacts",
	"tenant_skills",
	"tenant_skill_snapshots",
	"tenant_skill_catalog",
	"tenant_user_env_vars",
}
// expectedSQLiteMigrationVersion = 29
// + TestSQLiteMigrationsUpgradeV16AddsSessionForkColumns (test mới)
// + copySQLiteMigrationsThrough (tổng quát hoá, thay copySQLiteMigrationsV4)
```

### Phương án merge — lấy nguyên bản `main`, KHÔNG cần port gì từ develop
Toàn bộ nội dung `develop` thêm là **tập con** của những gì `main` đã có sẵn (5/16 bảng của develop đều nằm trong danh sách 16 bảng của main). Không có logic riêng nào của develop cần giữ lại ở file test này.

**Lưu ý quan trọng khác**: file test này chỉ verify test — logic thật (`internal/database/migration.go`, `internal/database/migration_source.go`) đã xác nhận **auto-merge sạch, không conflict** (xem `03-safe-list.md`). File test conflict chỉ vì nằm "gần" các dòng bị develop sửa, không phải vì kiến trúc composite migration của develop mâu thuẫn với cơ chế versioning của main.

### Việc cần làm
- [ ] Lấy nguyên bản `main` cho toàn bộ file.
- [ ] Chạy `go run ./cmd/migrate_validate postgres` và `... sqlite` sau merge (đã có sẵn từ phiên cô lập trước) để xác nhận composite migration source (bao gồm dải module `900000-909999` của develop) vẫn hoạt động đúng cùng version core mới (`main` hiện ở `000109`).
- [ ] Chạy `go test ./internal/database/...` toàn bộ.

---

## 7. `config/config.yaml` — `max_completion_tokens`, `max_input_chars`

### Xung đột
Giá trị số **thật sự đối lập nhau** trong cùng block `summary:`.

### Code develop (trước)
```yaml
    max_input_chars: 16384
    repeat_penalty: 1.0
    temperature: 0.3
    max_completion_tokens: 32768
```

### Code main (trước)
```yaml
  generate_kb_description_prompt_id: "default_kb_description"  # from prompt_templates/generate_kb_description.yaml
  ...
  summary:
    # Document profiles need the head of the document, not the whole body:
    # 8k characters is enough to name the topic, the type and one question.
    max_input_chars: 8192
    repeat_penalty: 1.0
    temperature: 0.3
    max_completion_tokens: 1024
```
Lưu ý: `main` có **comment giải thích lý do** chọn `8192` (đủ để nhận diện chủ đề tài liệu, không cần đọc hết). `develop`'s `16384` không có ghi chú lý do — khả năng cao là do quên hạ lại sau khi thử nghiệm, hoặc do nhu cầu tiếng Việt cần nhiều ký tự hơn (dấu, âm tiết) để nhận diện chủ đề.

### Phương án merge
1. **`max_input_chars`**: ưu tiên giữ `8192` của `main` — có lý do rõ ràng bằng comment, và đây là giới hạn *đầu vào* cho việc tạo document profile, không phải giới hạn chất lượng output. Nếu tiếng Việt thực sự cần nhiều ký tự hơn, nên tăng có kiểm soát (ví dụ `10240`) kèm comment giải thích, không nên giữ nguyên `16384` không rõ căn cứ.
2. **`max_completion_tokens`**: đây là **giá trị output**, không phải input — liên quan trực tiếp đến mục #5 (`custom_agent.go`) và mục #3 (`anthropic.go`). Quyết định ở đây phải nhất quán với quyết định ở mục #5: nếu chốt "chỉ agent cần completion dài mới set tường minh", thì `config.yaml`'s `summary.max_completion_tokens` (dùng cho tính năng tóm tắt, không phải agent completion) nên **giữ giá trị nhỏ của `main` (`1024`)** — tóm tắt không cần 32768 token.
3. Giữ thêm `generate_kb_description_prompt_id` (key mới của `main`, không có trên `develop` — xác nhận qua grep, develop hoàn toàn thiếu key này) — union, không mất gì.
4. Khối comment "Memory V2 configuration" (develop thêm ở cuối file config.yaml) — giữ nguyên, `main` không đụng vùng này nên union tự nhiên không mất gì.

### Việc cần làm
- [ ] Chốt giá trị `max_input_chars` — khuyến nghị lấy `8192` của `main` trừ khi có dữ liệu thực tế chứng minh tiếng Việt cần hơn.
- [ ] Chốt `max_completion_tokens` trong `summary:` block theo đúng quyết định đã chốt ở mục #5 (khả năng cao nên lấy `1024` của `main` vì đây là summary, không phải agent completion).
- [ ] Union `generate_kb_description_prompt_id` (main) + khối Memory V2 comment (develop).
- [ ] Chạy `go run ./cmd/server` (hoặc test load config) để xác nhận YAML hợp lệ và `ValidateConfig` không báo lỗi.

---

## 8. `internal/application/repository/retriever/weaviate/repository.go` — `tokenizeQuery`/`containsCJK`

### Xung đột
`develop` thêm `containsCJK` + branch `tokenizeQuery` (jieba cho CJK, whitespace-split cho ngôn ngữ khác — phục vụ tiếng Việt). `main` **xoá hẳn** `tokenizeQuery` khỏi file weaviate.

### Phát hiện quan trọng: code develop thêm là **dead code trong chính file này**
Đã xác minh: trên `develop`, không có lời gọi `tokenizeQuery(...)` nào trong `weaviate/repository.go` — hàm `KeywordsRetrieve` gọi thẳng `w.client.GraphQL().Bm25ArgBuilder().WithQuery(params.Query)` với chuỗi query thô, không qua `tokenizeQuery`. Tức là `tokenizeQuery`/`containsCJK` trên `develop` đã là hàm mồ côi (viết ra nhưng chưa từng nối vào luồng thật) từ trước khi merge này diễn ra.

### Code main (trước)
Không có `tokenizeQuery`/`containsCJK`. Trường schema vẫn khai báo `Tokenization: "gse"` — nghĩa là weaviate dựa vào tokenizer `gse` (built-in trong Weaviate) ở tầng server, không cần tokenize ở tầng application như `develop` định làm.

### Đối chiếu: logic CJK/jieba **vẫn tồn tại** trên `main`, nhưng ở repo khác
`internal/application/repository/retriever/qdrant/repository.go` (main) vẫn có `tokenizeQuery` đầy đủ dùng jieba (không có nhánh CJK-detect, luôn dùng jieba) — đây là nơi logic tokenize thật sự được dùng (`queryTokens := tokenizeQuery(params.Query)` tại dòng ~766).

### Phương án merge
1. **Weaviate**: lấy nguyên bản `main` (xoá `tokenizeQuery`/`containsCJK` khỏi weaviate) — vì code develop thêm vào đây chưa từng được dùng thật, xoá không mất chức năng nào đang chạy.
2. Nếu mục tiêu ban đầu của develop là "cải thiện tìm kiếm tiếng Việt trên Weaviate", cần làm việc này ở đúng chỗ: hoặc (a) thêm CJK-detect vào `qdrant/repository.go`'s `tokenizeQuery` (nơi logic thật sự chạy, áp dụng cho toàn bộ vector DB dùng qdrant), hoặc (b) nếu Weaviate cần xử lý riêng, phải **nối `tokenizeQuery` vào `KeywordsRetrieve`** thay vì chỉ định nghĩa hàm không gọi — đây là việc làm thêm ngoài phạm vi merge thuần tuý, nên tách thành task riêng sau merge.
3. Import `"unicode"`, `"unicode/utf8"` trên develop cũng nên bị xoá theo (chỉ tồn tại để phục vụ 2 hàm mồ côi này).

### Việc cần làm
- [ ] Lấy nguyên bản `main` cho `weaviate/repository.go` (bỏ hoàn toàn `tokenizeQuery`/`containsCJK`/2 import không dùng).
- [ ] Nếu vẫn cần cải thiện tìm kiếm tiếng Việt trên Weaviate: mở task riêng sau merge, tham khảo cách `qdrant/repository.go` đã làm (đã có jieba, có thể thêm branch CJK-detect tương tự).
- [ ] Chạy `go build ./...` để xác nhận không còn tham chiếu treo tới `tokenizeQuery` trong file weaviate.
- [ ] Chạy `go vet ./...` để bắt import không dùng nếu sót.

---

## 9. `frontend/src/views/knowledge/KnowledgeBase.vue` — tab Memory

### Xung đột
Không phải rename biến — **cấu trúc breadcrumb thật sự khác nhau**. `main` đã đơn giản hoá breadcrumb thành 1 khối `<h2 class="document-breadcrumb">` dùng chung `v-else class="breadcrumb-current"` không điều kiện; `develop` có breadcrumb 4-tab (`documents`/`wiki`/`graph`/`memory`) với cấu trúc `.breadcrumb-current { active: ... }` + `.breadcrumb-tab` lặp lại 2 lần khác với `main`.

### Code develop (trước — điểm neo chính)
```
113: const validTabs = ['documents', 'wiki', 'graph', 'memory'] as const
...
2371: <span :class="['breadcrumb-tab', { active: activeKbTab === 'memory' }]" ...>
2377: <span :class="['breadcrumb-current', { active: activeKbTab === 'documents' }]" ...>
2380: <span :class="['breadcrumb-tab', { active: activeKbTab === 'memory' }]" ...>
2422: <template v-if="activeKbTab === 'memory'">
      ... (toàn bộ nội dung tab Memory: subtabs Browse/Graph/Health/History + drawer)
```

### Code main (trước)
```
86:  const validTabs = ['documents', 'wiki', 'graph'] as const   // KHÔNG có 'memory'
2164: <h2 class="document-breadcrumb">
2171:   <button type="button" class="breadcrumb-link dropdown" :disabled="!kbId">
2181:   <button v-else type="button" class="breadcrumb-link" ...>
2191:   <span :class="['breadcrumb-tab', { active: activeKbTab === 'documents' }]" @click="activeKbTab = 'documents'">
2194:   <span :class="['breadcrumb-tab', { active: activeKbTab === 'wiki', indexing: wikiIsIndexing }]" @click="activeKbTab = 'wiki'">
2203:   <span :class="['breadcrumb-tab', { active: activeKbTab === 'graph', indexing: wikiIsIndexing }]" @click="activeKbTab = 'graph'">
2212:   <span v-else class="breadcrumb-current">{{ $t('knowledgeEditor.document.title') }}</span>
2243: <div v-if="isWiki && (activeKbTab === 'wiki' || activeKbTab === 'graph')" class="wiki-main-area">
2249: <template v-if="activeKbTab === 'documents' || !isWiki">
```
`main` cũng viết lại toàn bộ toolbar/filter-bar tài liệu (folder breadcrumb, search, tag-filter, batch-mode, skeleton loading) — vùng lân cận điểm chèn của develop bị thay đổi diện rộng.

### Phương án merge (thủ công, theo bước — KHÔNG dùng `git merge` tự động cho phần UI này)
1. Lấy toàn bộ file theo `main` làm nền (giữ mọi cải tiến UI mới: filter-bar, skeleton loading, batch-mode...).
2. Thêm `'memory'` vào mảng `validTabs` của `main`: `['documents', 'wiki', 'graph', 'memory'] as const`.
3. Trong khối breadcrumb mới của `main` (quanh dòng 2164-2212), thêm 1 `<span>` tab `memory` theo đúng pattern các tab khác của `main` đang dùng (`:class="['breadcrumb-tab', { active: activeKbTab === 'memory' }]" @click="activeKbTab = 'memory'"`), đặt sau tab cuối cùng hiện có (`graph`), **trước** dòng `<span v-else class="breadcrumb-current">`. Không copy nguyên khối breadcrumb cũ của `develop` — nó dùng cấu trúc `.breadcrumb-current { active: ... }` không còn khớp với `main`.
4. Thêm 1 khối nội dung mới `<template v-if="activeKbTab === 'memory'">...</template>` — **copy nguyên nội dung bên trong** khối tương ứng của `develop` (dòng 2422 trở đi: subtabs Browse/Graph/Health/History, `MemoryDrawer`, các import/computed liên quan ở đầu file `<script setup>`), đặt cạnh khối `<template v-if="activeKbTab === 'documents' || !isWiki">` của `main` — không lồng vào bên trong.
5. Copy các import/state ở đầu `<script setup>` từ develop: `useMemoryStore`, 4 view component (`MemoryBrowse`, `MemoryGraph`, `MemoryHealth`, `MemoryHistory`), `MemoryDrawer`, computed `subTabs`, state `memoryDrawerVisible`/`selectedMemory`, hàm `openMemoryDrawer`/`closeMemoryDrawer` — các phần này không đụng logic của `main`, an toàn để union nguyên vẹn.

### Việc cần làm
- [ ] Resolve theo 5 bước trên, làm bằng tay (không `git merge -X ours/theirs`).
- [ ] `cd frontend && npx vue-tsc --noEmit` — xác nhận không lỗi type.
- [ ] Chạy dev server, mở 1 Knowledge Base, kiểm tra bằng mắt: 4 tab đều hiển thị đúng breadcrumb style của `main` (không bị lệch CSS do trộn 2 class-structure), tab Memory mở đúng 4 sub-tab (Browse/Graph/Health/History), Drawer mở/đóng đúng.
- [ ] Kiểm tra riêng: `main`'s cải tiến filter-bar/batch-mode/skeleton loading vẫn hoạt động bình thường trên tab `documents` sau khi thêm tab `memory` (không bị vỡ layout do thêm 1 tab).

---

## Verification tổng thể sau khi resolve hết 9 mục trên

```bash
gofmt -l ./internal/ 2>&1 | grep -v -f <(git -C . ls-files | grep memory_v2)  # hoặc gofmt -w từng file vừa sửa
go build ./...
go run ./cmd/migrate_validate postgres
go run ./cmd/migrate_validate sqlite
go test ./internal/... 2>&1 | grep -E "FAIL|ok" 
cd frontend && npx vue-tsc --noEmit
```

Sau khi xanh toàn bộ, chạy lại dry-run merge để xác nhận không còn conflict nào sót:
```bash
git merge-tree --write-tree develop main | grep "^CONFLICT"   # phải rỗng nếu đã resolve hết trong 1 merge commit thật
```
