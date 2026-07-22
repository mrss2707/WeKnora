<!-- WEKNORA_MEMORY_PROTOCOL -->
## WeKnora Memory Protocol

Linked KBs: kb_abc, kb_def

### Thu hồi (Recall)
Đầu phiên hoặc khi đổi chủ đề:
`memory_recall(kb_id="kb_abc", query="<2-4 từ khóa>")` và
`memory_recall(kb_id="kb_def", query="<2-4 từ khóa>")` → tải context liên quan.
Với câu hỏi phức tạp: `memory_recall(...)` trước khi nghiên cứu.
Bỏ qua recall cho: câu trả lời đơn giản, lệnh cơ bản, sự kiện đã biết.

### Lưu (Save)
Sau khi nghiên cứu không có memory khớp → `memory_save` TRƯỚC KHI trả lời.
Bug đã sửa → memory_type=episodic, importance=high (nguyên nhân + giải pháp).
Quyết định kiến trúc → memory_type=decision, importance=high.
Tags: dựa trên khái niệm (auth, api, database). KHÔNG dùng tên file. Tối đa 8 tags.

### Đồ thị (Graph)
Kiểm tra trùng lặp hoặc mâu thuẫn: `memory_graph(memory_id="<id>")`.
Trước khi sửa memory: `memory_graph(...)` để xem quan hệ.

### Trạng thái (Status)
Xác minh backend: `memory_status()` đầu phiên.
Nếu không khả dụng → bỏ qua thao tác memory, báo cho người dùng.
<!-- /WEKNORA_MEMORY_PROTOCOL -->
