# 08 — MCP & Agent Integration

> Memory v2 Module | Last Update: 2026-07-09

## 1. Vấn đề: MCP Tool không có KB Context

### 1.1 Current State (Gap)

WeKnora hiện tại **không truyền KB context** cho MCP tools:

```
Agent Engine
  ├── CÓ KB context (ChatManage.PipelineRequest.KnowledgeBaseIDs)
  ├── CÓ tenant context (ctx → TenantIDContextKey)
  │
  └── MCPTool.Execute()
        └── MCPClient.CallTool(name, args)
              └── args ← KHÔNG chứa kb_id, tenant_id, hay session_id
```

Hậu quả: Nếu Memory v2 được expose qua MCP, agent gọi `memory_search("deploy")` sẽ không biết search trong KB nào.

### 1.2 SaiMem Cách Giải Quyết

SaiMem dùng `projectInfo.id` từ filesystem config — 1 project = 1 MCP server instance. Mỗi instance tự biết project của mình, không cần truyền từ bên ngoài. **Không áp dụng được cho WeKnora** vì WeKnora có multi-tenant, multi-KB trên cùng 1 server.

## 2. Giải pháp: Hai Integration Path

Memory v2 có **2 con đường** tích hợp vào agent, không phải chỉ 1:

```
┌─────────────────────────────────────────────────────────────────┐
│                     Memory v2 Integration                        │
├────────────────────────────┬────────────────────────────────────┤
│ PATH A: Internal Pipeline  │ PATH B: MCP Tool (optional)        │
│ (Primary — production)     │ (External agent access)             │
├────────────────────────────┼────────────────────────────────────┤
│ • Tích hợp trực tiếp vào   │ • Register Memory v2 như MCP tools │
│   chat pipeline           │ • memory_save, memory_search, etc. │
│ • Dùng ChatManage context │ • KB context injected tự động      │
│ • KB context có sẵn       │ • Agent không cần biết kb_id       │
│ • Zero overhead           │ • Dùng cho external MCP clients    │
├────────────────────────────┼────────────────────────────────────┤
│ Ưu tiên: CAO               │ Ưu tiên: THẤP (Phase 2)            │
└────────────────────────────┴────────────────────────────────────┘
```

## 3. Path A: Internal Pipeline (Primary)

### 3.1 Cách hoạt động

Memory v2 được tích hợp **trực tiếp** vào agent engine như 1 native tool, không qua MCP. KB context có sẵn từ `ChatManage`:

```go
// internal/agent/tools/memory_v2_tool.go — NEW FILE

type MemorySearchTool struct {
    memoryService interfaces.MemoryServiceV2
}

func (t *MemorySearchTool) Execute(ctx context.Context, input map[string]any) (string, error) {
    // KB context được resolve từ ChatManage trong context
    kbIDs := GetKBIDsFromContext(ctx)  // ← tự động từ pipeline
    
    query := input["query"].(string)
    filter := &types.MemoryFilter{
        KbID: kbIDs[0],  // primary KB
        Limit: 10,
    }
    
    results, err := t.memoryService.SearchMemories(ctx, query, filter)
    // ... format results for LLM consumption
}
```

### 3.2 KB Context Flow

```
HTTP Request → sessionService.AgentQA()
  → ChatManage.PipelineRequest.KnowledgeBaseIDs = ["kb-abc"]
  → agentService.CreateAgentEngine()
    → registerNativeMemoryTools(kbIDs)
      → MemorySearchTool{defaultKBID: "kb-abc"}
  → AgentEngine.Execute()
    → LLM: "gọi memory_search(query='deploy')"
    → MemorySearchTool.Execute() → search trong kb-abc
```

### 3.3 Native Tool Registration

```go
// internal/agent/tools/memory_v2_tool.go

func RegisterMemoryV2Tools(
    registry *ToolRegistry,
    memoryService interfaces.MemoryServiceV2,
    kbIDs []string,
) {
    defaultKBID := ""
    if len(kbIDs) > 0 {
        defaultKBID = kbIDs[0]
    }

    registry.Register(&ToolDef{
        Name:        "memory_search",
        Description: "Search memory trong knowledge base hiện tại. Dùng để tìm kiếm kiến thức đã lưu từ các phiên chat trước.",
        Parameters: map[string]any{
            "query": "Câu query để search memory",
            "type":  "Optional: episodic, semantic, procedural, decision, fact, preference",
        },
        Execute: func(ctx context.Context, input map[string]any) (string, error) {
            return executeMemorySearch(ctx, memoryService, defaultKBID, input)
        },
    })

    registry.Register(&ToolDef{
        Name:        "memory_save",
        Description: "Lưu một kiến thức/quyết định mới vào memory.",
        Parameters: map[string]any{
            "content": "Nội dung cần lưu",
            "type":    "Loại memory: episodic, semantic, procedural, decision, fact, preference",
        },
        Execute: func(ctx context.Context, input map[string]any) (string, error) {
            return executeMemorySave(ctx, memoryService, defaultKBID, input)
        },
    })
}
```

### 3.4 Ưu điểm của Path A

| Ưu điểm | Giải thích |
|---------|-----------|
| **KB context tự động** | KB ID được resolve từ session, agent không cần truyền |
| **Zero MCP overhead** | Không qua network, không serialize/deserialize |
| **Type safety** | Go type system, không qua JSON string |
| **Pipeline integration** | Memory retrieval tự động chạy trong MEMORY_RETRIEVAL stage |
| **Consistent error handling** | Dùng chung error pattern với các native tools khác |

## 4. Path B: MCP Tool với KB_ID Environment Variable

### 4.1 Pattern: 1 Instance = 1 KB

Thay vì auto-inject `kb_id` vào tool args (phức tạp, cần sửa engine), dùng pattern giống SaiMem: **mỗi MCP server instance được pre-scope vào 1 KB qua environment variable `WEKNORA_KB_ID`**.

```
┌──────────────────────────────────────────────────┐
│ MCP Client (Cursor, Claude Desktop, WeKnora)      │
│                                                    │
│  "weknora-memory-prod":                            │
│    env: WEKNORA_KB_ID=kb-production-abc            │
│    → tất cả tool calls tự động scoped vào KB này   │
│                                                    │
│  "weknora-memory-staging":                         │
│    env: WEKNORA_KB_ID=kb-staging-def               │
│    → tất cả tool calls tự động scoped vào KB này   │
└──────────────────────────────────────────────────┘
```

### 4.2 Cấu hình MCP Client

```json
{
  "mcpServers": {
    "weknora-memory": {
      "command": "/path/to/weknora-mcp-server/.venv/bin/python",
      "args": ["/path/to/weknora-mcp-server/main.py"],
      "env": {
        "WEKNORA_BASE_URL": "https://know.supermeo.com/api/v1",
        "WEKNORA_API_KEY": "sk-LVM8oFLg5L7lOzHCpvvCIjjkV6kqh6s2pX6FJ1QlqTSO19I5",
        "WEKNORA_KB_ID": "kb-production-abc"
      }
    }
  }
}
```

### 4.3 MCP Server — Đọc KB_ID từ Environment

```python
# mcp-server/main.py
import os

KB_ID = os.environ.get("WEKNORA_KB_ID")
BASE_URL = os.environ["WEKNORA_BASE_URL"]
API_KEY = os.environ["WEKNORA_API_KEY"]

# Validate KB belongs to tenant khi khởi động
def validate_kb():
    resp = requests.get(
        f"{BASE_URL}/knowledge-bases/{KB_ID}",
        headers={"Authorization": f"Bearer {API_KEY}"}
    )
    if resp.status_code != 200:
        raise RuntimeError(f"KB {KB_ID} not found or not authorized")

validate_kb()

# Tất cả tool handlers dùng KB_ID làm default scope
@server.tool()
async def memory_search(query: str, type: str = None, limit: int = 10):
    """Search memories in the configured knowledge base."""
    return await api.search_memories(
        kb_id=KB_ID,        # ← tự động, không cần truyền từ agent
        query=query,
        memory_type=type,
        limit=limit
    )

@server.tool()
async def memory_save(content: str, type: str = "episodic"):
    """Save a memory to the configured knowledge base."""
    return await api.save_memory(
        kb_id=KB_ID,        # ← tự động
        content=content,
        memory_type=type
    )
```

### 4.4 Ưu điểm so với Auto-Inject

| Tiêu chí | Auto-Inject (cũ) | KB_ID env (mới) |
|----------|-----------------|-----------------|
| **Engine change** | Cần sửa `MCPTool.Execute()` | **Không cần** |
| **External clients** | Chỉ WeKnora agent | **Cursor, Claude Desktop, mọi MCP client** |
| **Security** | LLM có thể chọn sai kb_id | **Server-enforced**, LLM không thể sai |
| **Độ phức tạp** | Cần context propagation | **1 dòng env** |
| **Multi-KB** | 1 connection, dynamic | N connections, mỗi cái 1 KB |
| **SaiMem alignment** | Không | **Giống hệt** (project-level scope) |

### 4.5 Khi nào cần Multi-KB

Nếu agent cần làm việc với nhiều KB:

```json
{
  "mcpServers": {
    "weknora-memory-prod": {
      "env": { "WEKNORA_KB_ID": "kb-production-abc" }
    },
    "weknora-memory-staging": {
      "env": { "WEKNORA_KB_ID": "kb-staging-def" }
    },
    "weknora-memory-docs": {
      "env": { "WEKNORA_KB_ID": "kb-documentation-ghi" }
    }
  }
}
```

Mỗi instance là 1 process Python nhẹ (~30MB RAM), có thể chạy hàng chục instances không vấn đề.

### 4.6 Security Validation

```python
# MCP server khởi động → validate KB ngay
# Nếu KB không tồn tại hoặc API key không có quyền → crash sớm

# Runtime: mọi API call dùng KB_ID cố định
# Không có cách nào để agent override KB_ID qua tool args
```

Ưu điểm bảo mật: KB scope được **server-enforced**, không phụ thuộc vào agent truyền đúng tham số. Đây là pattern "ambient authority" — quyền được cấp ở tầng infrastructure, không phải tầng application.

## 5. Tool Definitions

### 5.1 Native Tools (Path A — Primary)

| Tool Name | Description | Parameters |
|-----------|-------------|------------|
| `memory_search` | Search memories in current KB | `query` (required), `type` (optional), `limit` (optional) |
| `memory_save` | Save a new memory to current KB | `content` (required), `type` (optional) |
| `memory_recall` | Load relevant memories for context | `query` (required) — tự động gọi trong MEMORY_RETRIEVAL |
| `memory_graph` | Explore related memories | `memory_id` (required), `depth` (optional) |

### 5.2 MCP Tools (Path B — Phase 2)

Tất cả tools tự động scoped vào KB từ `WEKNORA_KB_ID` env. Không cần `kb_id` parameter.

| Tool Name | Description | Parameters |
|-----------|-------------|------------|
| `memory_search` | Search memories in configured KB | `query` (required), `type` (optional), `limit` (optional) |
| `memory_save` | Save memory to configured KB | `content` (required), `type` (optional) |
| `memory_recall` | Load relevant memories | `query` (required) |
| `memory_list` | Browse memories with pagination | `page`, `limit`, `type`, `verdict`, `tier` (all optional) |
| `memory_detail` | Get memory by ID | `memory_id` (required) |
| `memory_graph` | Explore memory graph | `memory_id` (required), `depth` (optional) |
| `memory_manage` | Update/delete/link memories | `action`, `memory_id`, various fields |
| `memory_health` | Get health report | (no params — auto-scoped to KB) |

## 6. System Prompt Integration

Agent system prompt đã chứa KB info. Khi Memory v2 active, thêm memory context:

```xml
<!-- Hiện tại — KB info -->
<knowledge_bases>
  <knowledge_base id="kb-abc" name="MyKB" type="document" doc_count="42" />
</knowledge_bases>

<!-- Thêm — Memory context -->
<memory_context>
  <results>
    <memory id="mem-1" type="procedural" importance="4" verdict="fixed" score="0.87">
      Deploy service mới: build Docker image → push registry → kubectl apply
    </memory>
  </results>
  <stats total="3" />
  <token_budget used="450" remaining="1550" mode="full" />
</memory_context>
```

LLM instruction:

```
You have access to a persistent memory system for this knowledge base.
When answering, consider both the knowledge base documents AND the memory context.
Use memory_search to find relevant past decisions, fixes, and procedures.
Use memory_save to record important new information for future sessions.
```

## 7. Decision Matrix

| Criteria | Path A (Internal) | Path B (MCP + KB_ID env) |
|----------|------------------|--------------------------|
| Latency | <5ms (in-process) | ~50ms (HTTP to WeKnora API) |
| KB context source | ChatManage auto | Environment variable (server-enforced) |
| Security | Go type safety | API key + KB validation at startup |
| Agent awareness | KB invisible to LLM | KB invisible to LLM |
| External clients | WeKnora only | **Any MCP client** |
| Engine changes | 0 (just register tools) | **0** |
| Multi-KB | 1 engine, dynamic | N MCP instances |
| SaiMem alignment | Different | **Identical pattern** |
| Production readiness | **Ready now** | Ready now |

**Khuyến nghị**: 
- **Production**: Path A (Internal Pipeline) cho chat flow chính
- **External access**: Path B với `WEKNORA_KB_ID` env cho Cursor/Claude Desktop/debugging
- **Cả 2 path cùng hoạt động** — không xung đột, không cần chọn 1 trong 2

## 8. Implementation Checklist

### Phase 1: Internal Pipeline (Required)

| # | Action | File |
|---|--------|------|
| 8.1 | Register `memory_search` native tool | `internal/agent/tools/memory_v2_tool.go` (new) |
| 8.2 | Register `memory_save` native tool | same file |
| 8.3 | Register `memory_recall` native tool | same file |
| 8.4 | Register `memory_graph` native tool | same file |
| 8.5 | Inject native tools vào `CreateAgentEngine()` | `internal/application/service/agent_service.go` |
| 8.6 | Resolve KB IDs từ `AgentConfig` | same file |
| 8.7 | Add memory context to system prompt | `internal/agent/prompts.go` |

### Phase 2: MCP Tools (Optional)

| # | Action | File |
|---|--------|------|
| 8.8 | Add KB auto-inject to `MCPTool.Execute()` | `internal/agent/tools/mcp_tool.go` |
| 8.9 | Create MCP server for memory tools | `internal/mcp/memory_server.go` (new) |
| 8.10 | Register memory tools as MCP service | `internal/mcp/manager.go` |
| 8.11 | Add `kb_id` validation on MCP server | same as 8.9 |

## 9. Verification

```bash
# Test native memory tools in agent
go test ./internal/agent/tools/memory_v2_tool_test.go -v

# Test agent with memory tools enabled
go test ./internal/agent/engine_test.go -v -run TestAgentWithMemoryTools

# Verify KB context flow
go test ./internal/application/service/agent_service_test.go -v -run TestMemoryV2KBContext
```
