# 06 — Risks & Mitigations

> Memory v2 Module | Last Update: 2026-07-09

## 1. Risk Matrix

| ID | Risk | Probability | Impact | Severity | Status |
|----|------|------------|--------|----------|--------|
| R1 | pgvector dimension mismatch | Medium | High | **High** | Mitigated |
| R2 | ParadeDB BM25 khác SQLite FTS5 | Medium | Medium | **Medium** | Mitigated |
| R3 | LLM extraction cost ở scale | High | Medium | **Medium** | Mitigated |
| R4 | Memory leak từ background goroutines | Medium | Medium | **Medium** | Mitigated |
| R5 | DB connection pool cạn | Medium | Medium | **Medium** | Mitigated |
| R6 | Race condition save/search | Low | Low | **Low** | Accepted |
| R7 | Migration fail trên DB có sẵn | Low | High | **Medium** | Mitigated |
| R8 | Conflict với MemoryService cũ | Low | Medium | **Low** | Mitigated |
| R9 | Multi-tenant data leak | Low | Critical | **High** | Mitigated |
| R10 | Performance 1M+ memories | Medium | Medium | **Medium** | Mitigated |
| R11 | Soft delete tích lũy | High | Low | **Low** | Mitigated |
| R12 | Dreamer LLM cost spike | Medium | Medium | **Medium** | Mitigated |
| R13 | Dreamer hallucinates wrong consolidations | Medium | High | **High** | Mitigated |
| R14 | Token budget overflow | Low | Medium | **Medium** | Mitigated |
| R15 | Verdict downgrade by LLM | Medium | Medium | **Medium** | Mitigated |

---

## 2. Detailed Analysis

### R1: pgvector Dimension Mismatch

**Risk**: Embedding model trả về dimension khác với `vector(N)` trong schema → INSERT fail.

**Probability**: Medium — thay đổi model embedding là operation phổ biến.

**Impact**: High — toàn bộ memory save/search bị fail.

**Mitigation**:
1. Migration dùng `vector(1536)` làm default (OpenAI text-embedding-3-small)
2. Code đọc dimension từ `embedder.GetDimensions()` trước khi gọi Save
3. Nếu dimension thay đổi → `ALTER TABLE agent_memories ALTER COLUMN embedding TYPE vector(N)` tự động
4. Thêm validation ở startup: check `embedder.GetDimensions()` == column dimension

**Detection**: Log warning nếu dimension mismatch ở lần gọi Save đầu tiên.

**Recovery**: Re-run migration với dimension mới, re-embed tất cả memories.

---

### R2: ParadeDB BM25 khác SQLite FTS5

**Risk**: ParadeDB pg_search có scoring function khác với SQLite FTS5 → kết quả search khác SaiMem reference.

**Probability**: Medium — search quality phụ thuộc vào BM25 implementation.

**Impact**: Medium — search results kém chính xác hơn expected.

**Mitigation**:
1. Calibrate weights riêng cho ParadeDB (default: BM25=0.15 thay vì 0.25)
2. Feature flag `memory_search_backend: paradedb` để dễ switch
3. A/B test search quality giữa paradedb và fallback (simple LIKE query)
4. Unit test verify BM25 scores có correlation với expected

**Detection**: Integration test so sánh search results giữa expected và actual.

**Recovery**: Switch sang `search_backend: simple` (LIKE query) nếu ParadeDB có vấn đề.

---

### R3: LLM Extraction Cost ở Scale

**Risk**: Entity extraction dùng LLM → tốn token, chi phí tăng theo số lượng memories.

**Probability**: High — mỗi memory mới đều trigger extraction.

**Impact**: Medium — tăng chi phí vận hành, không ảnh hưởng chức năng core.

**Mitigation**:
1. **Skip conditions**: content < 50 chars, memory_type = preference
2. **Batch processing**: Gom nhiều memories vào 1 LLM call (tương lai)
3. **Model rẻ**: Dùng Ollama local model hoặc model rẻ nhất (GPT-3.5-turbo)
4. **Rate limit**: Max 60 extraction requests/minute
5. **Queue depth monitoring**: Alert nếu queue > 100 pending
6. **Disable flag**: `ENABLE_ENTITY_EXTRACTION=false` để tắt hoàn toàn

**Detection**: Monitor `extraction_queue_depth` metric.

**Recovery**: Tắt entity extraction qua config flag nếu cost vượt ngân sách.

---

### R4: Memory Leak từ Background Goroutines

**Risk**: Background workers (entity extractor, consolidator, pruner) leak goroutines hoặc memory → service ngốn RAM.

**Probability**: Medium — long-running goroutines dễ bị leak nếu không handle context properly.

**Impact**: Medium — tăng RAM usage, cần restart service.

**Mitigation**:
1. **Context cancellation**: Tất cả workers nhận `ctx context.Context`, check `ctx.Done()` trong mỗi loop iteration
2. **Worker pool**: Dùng `ants` goroutine pool (max 5 workers)
3. **Graceful shutdown**: `Cleanup()` method gọi `cancel()` + `wg.Wait()`
4. **Panic recovery**: Mỗi worker có `defer recover()` để tránh crash toàn bộ service
5. **Health check**: Export worker status qua health endpoint

**Detection**:
- Monitor goroutine count (`runtime.NumGoroutine()`)
- Memory profile mỗi 6h
- Alert nếu goroutine > 100

**Recovery**: Restart service (workers tự restart qua DI container).

---

### R5: DB Connection Pool Cạn

**Risk**: Memory workers chiếm connections, ảnh hưởng đến main application queries.

**Probability**: Medium — PG connection pool default thường nhỏ (10-20).

**Impact**: Medium — application chậm, timeout.

**Mitigation**:
1. **Separate pool**: Workers dùng pool riêng (max 5 connections)
2. **Tái dùng connection**: Dùng cùng `*gorm.DB` instance qua DI
3. **Connection timeout**: Set `pool_timeout` để fail fast thay vì block
4. **pgBouncer**: Recommend trong production deployment

**Detection**: Monitor `pg_stat_activity` count.

**Recovery**: Kill idle connections, restart workers.

---

### R6: Race Condition giữa Save và Search

**Risk**: Memory vừa save chưa kịp index → search không thấy.

**Probability**: Low — PostgreSQL READ COMMITTED isolation đảm bảo visibility.

**Impact**: Low — memory sẽ xuất hiện trong search tiếp theo.

**Mitigation**:
1. READ COMMITTED isolation (PostgreSQL default)
2. ivfflat index tự động update khi INSERT (không cần REINDEX)
3. BM25 index cập nhật real-time (ParadeDB)

**Detection**: N/A — accepted risk.

**Recovery**: Không cần (tự resolve trong search tiếp theo).

---

### R7: Migration Fail Trên DB Có Sẵn

**Risk**: Migration 000064 fail do conflict với schema hiện tại hoặc thiếu extension.

**Probability**: Low — migration dùng `IF NOT EXISTS`.

**Impact**: High — app không start được.

**Mitigation**:
1. Tất cả DDL dùng `IF NOT EXISTS` / `IF EXISTS`
2. `.down.sql` sẵn sàng để rollback
3. Extension check: `CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS pg_search;`
4. Test migration trên staging DB trước khi deploy production
5. Migration được đánh số sequential (000064), chạy theo thứ tự

**Detection**: CI pipeline chạy migration trên test DB.

**Recovery**: Run `.down.sql` → fix issue → re-run `.up.sql`.

---

### R8: Conflict Với MemoryService Cũ

**Risk**: Cả MemoryService (Neo4j) và MemoryServiceV2 (PG) cùng implement `MemoryService` interface → DI container không biết chọn cái nào.

**Probability**: Low — container chỉ register 1 implementation.

**Impact**: Medium — app có thể dùng sai service.

**Mitigation**:
1. Container chỉ register **1** implementation của `MemoryService` interface
2. Config flag `MEMORY_BACKEND=v2` để chọn implementation
3. `MemoryServiceV2` implements BOTH `MemoryService` (backward compat) AND `MemoryServiceV2` (new methods)
4. Plugin (`MemoryPlugin`) injection type là `interfaces.MemoryService` → không cần thay đổi plugin code

**Detection**: Startup log ghi rõ "memory backend: v2 (postgresql+pgvector)".

**Recovery**: Set `MEMORY_BACKEND=neo4j` để quay về implementation cũ.

---

### R9: Multi-Tenant Data Leak

**Risk**: Memory của tenant A bị lộ cho tenant B qua search query.

**Probability**: Low — tất cả query có `tenant_id` filter.

**Impact**: **Critical** — security breach, data leak.

**Mitigation**:
1. **`tenant_id` trong mọi WHERE clause** — bắt buộc ở repository layer
2. **Composite index** `(tenant_id, kb_id)` đảm bảo index scan luôn filter by tenant
3. **Repository middleware** — `tenant_id` luôn được set từ context, không từ user input
4. **Unit test** — verify query có `tenant_id` filter
5. **Integration test** — 2 tenants, verify không cross-access

**Detection**: Audit log tất cả memory access. Integration test cross-tenant.

**Recovery**: Nếu phát hiện leak → hotfix query, audit log để xác định scope.

---

### R10: Performance 1M+ Memories

**Risk**: Search performance degrade khi số lượng memories vượt 1M.

**Probability**: Medium — growth tự nhiên theo thời gian.

**Impact**: Medium — search latency >500ms.

**Mitigation**:
1. **HNSW index**: m=16, ef_construction=200 → O(log N) search thay vì O(sqrt(N)) ivfflat
2. **Pre-computed hub_score**: Tránh recursive CTE trong hot search path
3. **Partition**: Partition `agent_memories` theo `tenant_id` khi >10M rows
4. **LIMIT trước scoring**: Filter 100 candidates trước khi full scoring
5. **Parallel queries**: BM25 + HNSW + Graph chạy đồng thời
6. **Embedding cache**: LRU cache query embeddings, TTL 5 minutes
7. **Result cache**: Cache search results per query hash, TTL 2 minutes
8. **Score threshold**: Drop results <0.4 trước context packing
9. **Index maintenance**: `REINDEX` định kỳ (monthly, HNSW ít cần hơn ivfflat)

**Detection**: Monitor p99 search latency, alert nếu >500ms.

**Recovery**: Tăng `ef_search`, partition table, scale up DB.

---

### R11: Soft Delete Tích Lũy

**Risk**: Soft-deleted memories tích lũy → DB size tăng, search performance giảm.

**Probability**: High — soft delete là cơ chế chính.

**Impact**: Low — ảnh hưởng từ từ, dễ quản lý.

**Mitigation**:
1. **Pruner daily job**: Hard-delete tier-3 soft-deleted >14d, access_count=0
2. **VACUUM monthly**: `VACUUM ANALYZE agent_memories`
3. **Retention policy**: Tier-1/2 soft-deleted keep 90d, tier-3 keep 14d
4. **Monitor**: DB size trend, alert nếu growth >20%/month

**Detection**: Monitor `pg_total_relation_size('agent_memories')`.

**Recovery**: Manual cleanup query nếu cần khẩn cấp.

---

### R12: Dreamer LLM Cost Spike

**Risk**: Dreamer chạy mỗi 1h cho mỗi tenant, dùng LLM → chi phí tăng theo số tenant × memories.

**Probability**: Medium — số lượng tenant và memories tăng theo thời gian.

**Impact**: Medium — tăng chi phí vận hành, không ảnh hưởng chức năng core.

**Mitigation**:
1. **Model rẻ**: Dùng GPT-3.5-turbo hoặc Ollama local model
2. **Gate chặt**: Min 1h between passes, lock-based (không concurrent)
3. **Budget cứng**: Max 4000 tokens/pass, max 5 actions/pass
4. **Sampling**: Không analyze toàn bộ memories — sample top 50 most active
5. **Disable flag**: `DREAMER_ENABLED=false` để tắt hoàn toàn
6. **Tenant-level opt-in**: Chỉ enable dreamer cho tenant có >100 memories

**Detection**: Monitor `dream_token_used` per pass, alert nếu >4000.

**Recovery**: Tắt dreamer qua config flag, revert về schedule-only consolidation.

---

### R13: Dreamer Hallucinates Wrong Consolidations

**Risk**: Dreamer LLM đề xuất merge/delete sai → mất dữ liệu quan trọng.

**Probability**: Medium — LLM hallucination là rủi ro đã biết.

**Impact**: **High** — Mất memory quan trọng, sai verdict.

**Mitigation**:
1. **Validation layer**: Tất cả actions phải qua `parseAndValidateActions()` — check protected verdicts, tier constraints, confidence threshold (≥0.70)
2. **Dry-run mode**: `DREAMER_DRY_RUN=true` → preview actions without applying
3. **Rollback capability**: Soft-delete thay vì hard-delete cho dreamer-proposed deletions
4. **Protected verdicts**: `decision`, `fixed` không bao giờ bị LLM touch
5. **Confidence threshold**: Chỉ apply actions với confidence ≥0.70
6. **Manual review queue**: Actions với confidence 0.70-0.85 → queue cho human review
7. **Audit log**: Log tất cả dreamer actions với before/after state

**Detection**: Compare memory count before/after dream pass. Alert if >5% change.

**Recovery**: Restore từ soft-delete, revert verdict changes từ audit log.

---

### R14: Token Budget Overflow

**Risk**: Memory context vượt quá token budget → LLM context window overflow, response bị truncate.

**Probability**: Low — có hard cap và fallback modes.

**Impact**: Medium — Chat response bị cắt hoặc thiếu context.

**Mitigation**:
1. **Hard cap**: `MaxTotalTokens = 2000` — không bao giờ vượt quá
2. **Truncated mode**: Tự động switch khi >1500 tokens (cap 300 tokens/memory)
3. **Summary mode**: Tự động switch khi >2500 tokens (LLM summarize)
4. **Token estimation**: Dùng heuristic (4 chars ≈ 1 token) cho estimation nhanh, không cần tokenizer
5. **Budget monitoring**: Log `token_used` trong mỗi response XML
6. **Reserve for system**: Luôn để dành ≥500 tokens cho system prompt

**Detection**: Monitor `token_used` trong `<token_budget>` XML tag.

**Recovery**: Tăng `max_total_tokens` config, hoặc giảm `max_search_results`.

---

### R15: Verdict Downgrade by LLM

**Risk**: Dreamer hoặc entity extractor vô tình thay đổi verdict của memory quan trọng (vd: `decision` → `none`).

**Probability**: Medium — LLM không hiểu ngữ cảnh verdict system.

**Impact**: Medium — Memory quan trọng bị downgrade → search không ưu tiên đúng.

**Mitigation**:
1. **Protected verdicts**: `decision`, `fixed` — không LLM nào được phép thay đổi
2. **Pre-write guard**: `MemoryRepositoryV2.Update()` check `verdict.IsProtected()` trước khi save
3. **Verdict change audit**: Log mọi thay đổi verdict với actor (user/system/dreamer)
4. **Human-only transitions**: `none → decision` và `decision → *` chỉ user/admin được thực hiện
5. **Dreamer constraint**: Prompt explicitly says "Do NOT touch memories with verdict 'decision' or 'fixed'"

**Detection**: Audit log alert nếu protected verdict bị thay đổi bởi non-human actor.

**Recovery**: Revert verdict từ audit log, investigate dreamer prompt.

---

## 3. Rollback Plan

Nếu Memory v2 gây vấn đề nghiêm trọng trong production:

### Step 1: Disable Memory v2 (immediate)
```bash
# Set env var
MEMORY_BACKEND=neo4j
# Hoặc disable memory hoàn toàn
ENABLE_MEMORY=false
```

### Step 2: Rollback code (nếu cần)
```bash
git revert <commit-hash>
```

### Step 3: Rollback migration (nếu cần)
```bash
make migrate-down  # Runs migrations in reverse: 000066 → 000065 → 000064
```

### Step 4: Clean up data (optional)
```sql
DROP TABLE IF EXISTS extraction_queue;
DROP TABLE IF EXISTS memory_relations;
DROP TABLE IF EXISTS agent_memories;
```

---

## 4. Monitoring Checklist

| What to monitor | Tool | Alert threshold |
|----------------|------|-----------------|
| Search latency p99 | Prometheus + Grafana | >500ms |
| Save latency p99 | Prometheus + Grafana | >1s |
| Extraction queue depth | App metric | >100 pending |
| Extraction failure rate | App metric | >10% |
| Dreamer token used/pass | App metric | >4000 tokens |
| Dreamer actions applied | App metric | >5/pass |
| Dreamer failure rate | App metric | >3/hour |
| Health issues critical | App metric | >0 |
| Token budget overflow | App log | Any overflow event |
| Cache hit rate | App metric | <50% |
| DB connection count | pg_stat_activity | >80% pool |
| Goroutine count | runtime.NumGoroutine() | >100 |
| DB table size | pg_total_relation_size | Growth >20%/month |
| Verdict changes by non-human | Audit log | Any occurrence |
| Memory access errors | App log | Any ERROR level |
| Cross-tenant access | Audit log | Any occurrence |

---

## 5. Contingency Contacts

| Issue | Contact | Escalation |
|-------|---------|------------|
| DB performance | DBA team | After 30min unresolved |
| LLM cost spike | ML/Infra team | After 1h unresolved |
| Data leak suspected | Security team | **Immediate** |
| Service down | On-call engineer | After 5min |
