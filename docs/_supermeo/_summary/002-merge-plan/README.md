# Merge plan: `main` → `develop`

**Ngày chuẩn bị**: 2026-09-22
**Merge-base**: `412dcc41c662c9b45698959e0c3c37db5b8dc9d3` (2026-08-21)
**Khoảng cách**: `main` đi trước 397 commit / 2651 file / +330k dòng; `develop` đi trước 107 commit / 225 file / +49.5k dòng.
**Phương pháp xác minh**: `git merge-tree --write-tree develop main` (dry-run thật qua Git, không đụng working tree) — kết quả: **38 file conflict** (35 content, 2 modify/delete, 1 add/add).

## Cách dùng thư mục này

- `README.md` (file này) — tổng quan, thứ tự resolve khuyến nghị, câu lệnh chạy dry-run merge để tái tạo danh sách conflict.
- `01-checklist.md` — checklist chi tiết cho **8 file rủi ro cao (🔴)** đã xác định trong buổi rà soát trước: mỗi file có code trước/sau của cả 2 nhánh, nguyên nhân xung đột, và phương án resolve cụ thể để logic của cả `main` lẫn `develop` cùng vận hành đúng sau merge (không phải "chọn 1 bên").
- `02-checklist-medium.md` — checklist ngắn hơn cho các file rủi ro trung bình (🟡) có khả năng regression nếu resolve ẩu, đặc biệt `message.go`.
- `03-safe-list.md` — danh sách các vùng đã xác nhận **không** conflict hoặc auto-merge sạch, để không tốn công kiểm tra lại.

## Tái tạo dry-run merge (không đụng working tree)

```bash
git merge-base main develop   # xác nhận merge-base vẫn là 412dcc41...
git merge-tree --write-tree develop main > /tmp/merge_preview.txt
grep "^CONFLICT" /tmp/merge_preview.txt
```

Lệnh này **an toàn tuyệt đối** — không tạo commit, không đổi working tree, không đổi HEAD. Chạy lại bất cứ lúc nào để xác nhận danh sách conflict còn đúng trước khi bắt tay resolve thật (cả hai nhánh có thể đã có thêm commit từ lúc viết tài liệu này).

## Thứ tự resolve khuyến nghị

1. **Trước tiên**: `internal/database/migration_sqlite_versioned_schema_test.go` (file #6 trong checklist) — không có logic nghiệp vụ rủi ro, chỉ là "áp dụng lại 5 dòng của develop vào bản test đã mở rộng của main". Làm trước để xác nhận môi trường build/test hoạt động đúng ngay từ đầu.
2. **Sau đó, theo cụm liên quan**:
   - Cụm "model & completion tokens": `internal/models/chat/anthropic.go` (#3) → `internal/models/embedding/embedder.go` (#4) → `internal/types/custom_agent.go` (#5) → `config/config.yaml` (#7). Bốn file này cùng một chủ đề (giá trị mặc định token/model), nên resolve liền nhau để giữ nhất quán.
   - Cụm "message persistence": `internal/application/repository/message.go` (#1) → `internal/handler/session/qa.go` (#2). Phụ thuộc lẫn nhau (qa.go gọi UpdateMessage).
   - Riêng lẻ: `internal/application/repository/retriever/weaviate/repository.go` (#8) — độc lập, ít rủi ro thực (code develop thêm chủ yếu chết/không được gọi trong file này).
3. **Cuối cùng, khó nhất**: `frontend/src/views/knowledge/KnowledgeBase.vue` (#9 trong checklist chính) — cần UI có sẵn để test bằng mắt sau khi resolve, nên để sau khi backend đã build/test xanh.
4. Sau khi resolve xong tất cả: chạy full verification ở cuối `01-checklist.md`.

## Ghi chú quan trọng phát hiện trong lúc chuẩn bị

- **`internal/models/chat/anthropic.go` bị `main` xoá hoàn toàn** — không phải conflict nội dung, mà là modify/delete. Toàn bộ kiến trúc provider chat cũ (`internal/models/chat/*`) đã được `main` thay bằng hệ `internal/models/api/<protocol>` + `internal/models/catalog` + `internal/models/vendors`. Giá trị `MaxTokens: 32768` của develop phải "di cư" sang field khác hẳn (`catalog.DefaultAnthropicMessages().DefaultMaxTokens`), không thể giữ nguyên file.
- **`internal/models/embedding/embedder.go`**: main đã xoá toàn bộ switch theo provider (~160 dòng) và case `"generic"` develop thêm không còn chỗ bám — kiến trúc catalog/vendor mới của main xử lý "generic" như một **vendor ID** (`catalog.GenericID`), không phải `types.ModelSource`. Không thể port cơ học.
- **`internal/types/custom_agent.go`**: đây là **xung đột mô hình thiết kế thật**, không chỉ giá trị. `develop` materialize default `32768` ngay lúc lưu config; `main` cố tình bỏ việc này, để `0` nghĩa là "dùng default theo ngữ cảnh lúc gọi" (2048/4096/24576 tuỳ agent mode + có sandbox hay không, qua `types.AgentRoundMaxCompletionTokensFor`). Nếu resolve máy móc giữ nguyên khối `if == 0 { = 32768 }` của develop, sẽ **phá vỡ test có sẵn của main** (`TestEnsureDefaults_MaxCompletionTokensByMode`) và xoá bỏ toàn bộ thiết kế graduated-default mới.
- **`internal/application/repository/message.go`**: main đã viết lại `UpdateMessage` thành transaction + `writeMessageArtifacts`, nhưng **vẫn dùng `.Updates(message)` chứ không phải `.Select("*")`** — nghĩa là bug gốc mà develop từng fix (GORM bỏ qua zero-value field khi Update, gây "empty chat response" — xem commit `8b97f164`) **vẫn còn tồn tại nguyên trên `main`**. Đây là điểm dễ bị bỏ sót nhất trong toàn bộ merge — nếu người resolve chỉ lấy nguyên bản `main`, bug sẽ quay lại mà không có conflict nào báo hiệu (vì `.Select("*")` là 1 method chain thêm vào, git có thể tự merge nhầm nếu vị trí dòng trùng khớp).
- Các vùng đã cô lập tốt từ trước (`internal/container/container.go`, `internal/router/router.go` core registration, `internal/database/migration.go`, `migration_source.go`, dải version `900000-909999`) — **xác nhận không có conflict nào phát sinh**, không cần đưa vào checklist chi tiết.
