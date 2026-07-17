# 07 — UI/UX Design

> Memory v2 Module | Last Update: 2026-07-09

## 1. Architectural Decision: Memory = KB Tab

### 1.1 Why Memory Belongs Inside Knowledge Base

Memory v2 được thiết kế với nguyên lý "coi KB như 1 project". Mỗi KB có memory riêng, giống như mỗi KB có documents, wiki, graph riêng. Đặt Memory làm tab thứ 4 trong KB detail page là kiến trúc chính xác vì:

| Lý do | Giải thích |
|-------|-----------|
| **Data scope** | Memory được lưu với `kb_id`, tất cả query đều filter theo KB |
| **Mental model** | User vào 1 KB → xem tất cả dữ liệu của KB đó (docs, wiki, graph, memory) |
| **Consistency** | Documents, Wiki, Graph đều là KB tabs → Memory cũng nên là KB tab |
| **SaiMem pattern** | SaiMem scopes memory theo `project` (tương đương KB trong WeKnora) |
| **Tránh fragmentation** | Nếu Memory là route riêng, user phải context-switch giữa docs và memories |

### 1.2 What WeKnora Already Has

Hiện tại `KnowledgeBase.vue` có 3 tab trong breadcrumb:

```
Knowledge Bases > [KB Name] > Documents / Wiki / Graph
```

Tabs được switch bằng `activeKbTab` ref, sync với `?tab=` query param. Memory sẽ là tab thứ 4:

```
Knowledge Bases > [KB Name] > Documents / Wiki / Graph / Memory
```

### 1.3 SaiMem Patterns Adopted

Từ SaiMem webview (4 tabs: Database, History, Graph, Statistics), Memory v2 áp dụng:

| SaiMem | Memory v2 | Ghi chú |
|--------|-----------|---------|
| Database | **Browse** (sub-tab) | Table + card grid toggle, filters, bulk ops |
| History | **History** (sub-tab) | Activity timeline: created, updated, verdict changes, dreamer actions |
| Graph | **Graph** (sub-tab) | Reuse WikiBrowser SVG graph, memory-specific styling |
| Statistics | **Health** (sub-tab) | Metric cards + chart + issue list |

## 2. Kiến trúc tổng quan

```
┌──────────────────────────────────────────────────────────────────┐
│ Sidebar                          │ KnowledgeBase.vue              │
│ ┌──────────────────────────────┐ │ ┌────────────────────────────┐ │
│ │ [New Chat]                   │ │ │ Knowledge Bases > MyKB >  │ │
│ │ [Knowledge Bases] ← active   │ │ │ Documents|Wiki|Graph|Memory│ │
│ │ [Agents]                     │ │ └────────────────────────────┘ │
│ │ [Organizations]              │ │ ┌────────────────────────────┐ │
│ │ ────────────────             │ │ │                            │ │
│ │ [Settings]                   │ │ │  activeKbTab === 'memory'  │ │
│ │                              │ │ │                            │ │
│ │                              │ │ │  ┌─ Sub-tabs ───────────┐ │ │
│ │                              │ │ │  │ Browse│Graph│Health  │ │ │
│ │                              │ │ │  │       │     │History │ │ │
│ │                              │ │ │  └──────────────────────┘ │ │
│ │                              │ │ │                            │ │
│ │                              │ │ │  [Sub-tab content area]    │ │
│ │                              │ │ │                            │ │
│ └──────────────────────────────┘ │ └────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

**Không có route `/memory` riêng** — Memory luôn nằm trong context của 1 KB.

## 3. Routes & Integration Points

### 3.1 Router

```typescript
// KHÔNG thêm route mới. Memory dùng route hiện tại:
// /platform/knowledge-bases/:kbId?tab=memory

// Chỉ cần thêm 'memory' vào validTabs trong KnowledgeBase.vue
const validTabs = ['documents', 'wiki', 'graph', 'memory'] as const
```

### 3.2 Sidebar Menu

**Không thay đổi**. Khi user đang ở KB detail page (bất kể tab nào), sidebar "Knowledge Bases" vẫn được highlight.

### 3.3 KB Context

Memory tab dùng chung `kbId` từ route params, `kbInfo` từ existing API, permissions từ existing RBAC:

```typescript
// Trong KnowledgeBase.vue
const kbId = computed(() => (route.params as any).kbId as string || '');
const { isOwner, canEdit, canMutateKnowledge } = useKBPermissions(kbId);
```

### 3.4 API Module

```typescript
// frontend/src/api/memory/index.ts

// Tất cả endpoints tự động include kb_id từ query parameter
export function listMemories(kbId: string, params: MemoryListParams): Promise<PaginatedResponse<Memory>>
export function getMemory(kbId: string, memoryId: string): Promise<Memory>
export function createMemory(kbId: string, data: CreateMemoryRequest): Promise<SaveMemoryResult>
export function updateMemory(kbId: string, memoryId: string, data: UpdateMemoryRequest): Promise<Memory>
export function deleteMemory(kbId: string, memoryId: string): Promise<void>
export function searchMemories(kbId: string, params: SearchParams): Promise<MemorySearchResult[]>
export function getMemoryGraph(kbId: string, memoryId: string, depth: number): Promise<GraphData>
export function getMemoryStats(kbId: string): Promise<MemoryStats>
export function getHealthReport(kbId: string): Promise<HealthReport>
export function getMemoryHistory(kbId: string, params: HistoryParams): Promise<HistoryEntry[]>
export function triggerDream(kbId: string): Promise<DreamResult>
export function updateVerdict(kbId: string, memoryId: string, verdict: string): Promise<Memory>
export function bulkUpdateVerdict(kbId: string, memoryIds: string[], verdict: string): Promise<void>
export function bulkDelete(kbId: string, memoryIds: string[]): Promise<void>
export function exportMemories(kbId: string, format: 'json'|'csv'): Promise<Blob>
```

### 3.5 Store

```typescript
// frontend/src/stores/memory.ts — Pinia store
export const useMemoryStore = defineStore('memory', () => {
  const kbId = ref<string>('')
  const memories = ref<Memory[]>([])
  const currentMemory = ref<Memory | null>(null)
  const stats = ref<MemoryStats | null>(null)
  const graphData = ref<GraphData | null>(null)
  const healthReport = ref<HealthReport | null>(null)
  const dreamResult = ref<DreamResult | null>(null)
  const historyEntries = ref<HistoryEntry[]>([])
  
  // Filters + pagination
  const filter = reactive<MemoryFilter>({})
  const pagination = reactive({ page: 1, limit: 20, total: 0 })
  const viewMode = ref<'grid' | 'table'>('grid')
  const activeSubTab = ref<'browse' | 'graph' | 'health' | 'history'>('browse')
  const selectedIds = ref<Set<string>>(new Set())

  // Getters
  const verdictCounts = computed(() => ...)
  const typeCounts = computed(() => ...)
  const hasSelection = computed(() => selectedIds.value.size > 0)

  // Actions
  async function loadMemories(kbId: string) { ... }
  async function loadMemory(kbId: string, id: string) { ... }
  async function loadGraph(kbId: string, centerId?: string) { ... }
  async function loadHealthReport(kbId: string) { ... }
  async function loadHistory(kbId: string) { ... }
  async function runDreamer(kbId: string) { ... }
  function setSubTab(tab: 'browse' | 'graph' | 'health' | 'history') { ... }
  function setViewMode(mode: 'grid' | 'table') { ... }
  function toggleSelect(id: string) { ... }
  function selectAll() { ... }
  function clearSelection() { ... }
})
```

## 4. Memory Tab — Tích hợp vào KnowledgeBase.vue

### 4.1 Tab Declaration

```typescript
// Thêm vào KnowledgeBase.vue
const validTabs = ['documents', 'wiki', 'graph', 'memory'] as const
type KbTab = typeof validTabs[number]
```

### 4.2 Tab Rendering

Breadcrumb tabs được render như các tab khác:

```html
<!-- KnowledgeBase.vue — line ~2042, thêm sau Graph tab -->
<span 
  v-if="isMemoryEnabled" 
  :class="{ active: activeKbTab === 'memory' }" 
  class="kb-tab-item"
  @click="switchTab('memory')"
>
  Memory
  <t-badge v-if="dreamerPendingCount > 0" :count="dreamerPendingCount" size="small" />
</span>
```

### 4.3 Tab Content Area

```html
<!-- Memory tab content — thêm sau Wiki/Graph block -->
<template v-if="activeKbTab === 'memory'">
  <div class="memory-main-area">
    <!-- Sub-tab navigation -->
    <div class="memory-subtabs">
      <span 
        v-for="sub in subTabs" :key="sub.key"
        :class="{ active: memoryStore.activeSubTab === sub.key }"
        @click="memoryStore.setSubTab(sub.key)"
      >
        <t-icon :name="sub.icon" size="16px" />
        {{ sub.label }}
        <t-badge v-if="sub.badge" :count="sub.badge" size="small" />
      </span>
      <div class="memory-subtab-actions">
        <t-button v-if="memoryStore.activeSubTab === 'browse'" 
                  size="small" variant="outline" @click="memoryStore.setViewMode(viewMode === 'grid' ? 'table' : 'grid')">
          <t-icon :name="viewMode === 'grid' ? 'view-list' : 'view-module'" />
        </t-button>
      </div>
    </div>

    <!-- Sub-tab content -->
    <KeepAlive>
      <MemoryBrowse  v-if="memoryStore.activeSubTab === 'browse'"  :kb-id="kbId" />
      <MemoryGraph   v-if="memoryStore.activeSubTab === 'graph'"   :kb-id="kbId" />
      <MemoryHealth  v-if="memoryStore.activeSubTab === 'health'"  :kb-id="kbId" />
      <MemoryHistory v-if="memoryStore.activeSubTab === 'history'" :kb-id="kbId" />
    </KeepAlive>
  </div>
</template>
```

**Sub-tabs definition**:
```typescript
const subTabs = computed(() => [
  { key: 'browse',  icon: 'view-list',    label: t('memory.subTabs.browse'),  badge: null },
  { key: 'graph',   icon: 'chart-bubble', label: t('memory.subTabs.graph'),   badge: null },
  { key: 'health',  icon: 'heart',        label: t('memory.subTabs.health'),  
    badge: healthReport.value?.totalIssues || null },
  { key: 'history', icon: 'time',         label: t('memory.subTabs.history'), badge: null },
])
```

## 5. Sub-Tab 1: Browse

**Component**: `frontend/src/views/memory/MemoryBrowse.vue`

### 5.1 Layout

```
┌──────────────────────────────────────────────────────────────────────┐
│ [Browse] [Graph] [Health ●3] [History]              [Grid|Table] [+] │
├──────────────────────────────────────────────────────────────────────┤
│ Filters: [Type ▾] [Verdict ▾] [Tier ▾] [Tags ▾]  🔍 Search...      │
├──────────────────────────────────────────────────────────────────────┤
│ ☐ Select All  [Bulk: Update Verdict ▾] [Bulk: Delete]  (5 selected) │  ← bulk bar
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Grid View                           Table View                      │
│  ┌──────────────┐ ┌──────────────┐   ┌────────┬────────┬──────┐    │
│  │⚠ decision    │ │✅ fixed      │   │☐ Verdict│Type   │Cont..│    │
│  │ Deploy qua.. │ │ Bug X has..  │   │☐ dec    │proced │...   │    │
│  │ 3d · T1 ·4⭐ │ │ 7d · T0 ·5⭐ │   │☐ fixed  │proced │...   │    │
│  └──────────────┘ └──────────────┘   │☐ none   │semant │...   │    │
│                                                                      │
│ ← 1 2 3 ... 6 →  20 per page                                        │
└──────────────────────────────────────────────────────────────────────┘
```

### 5.2 Card View (Default)

```vue
<!-- MemoryCard.vue — reuses KnowledgeBase card pattern -->
<div 
  class="memory-card" 
  :class="[`memory-card--${memory.verdict}`, { 'memory-card--selected': isSelected }]"
  @click="openDetail(memory.id)"
>
  <div class="memory-card__checkbox" @click.stop="toggleSelect(memory.id)">
    <t-checkbox :checked="isSelected" />
  </div>
  
  <div class="memory-card__header">
    <t-tag :theme="verdictTheme(memory.verdict)" variant="light" size="small">
      <t-icon :name="verdictIcon(memory.verdict)" size="12px" />
      {{ $t(`memory.verdict.${memory.verdict}`) }}
    </t-tag>
    <t-tag variant="outline" size="small">
      T{{ memory.tier }}
    </t-tag>
    <span class="memory-card__stars">{{ '⭐'.repeat(memory.importance) }}</span>
  </div>

  <p class="memory-card__content">{{ truncate(memory.content, 150) }}</p>

  <div class="memory-card__footer">
    <span class="memory-card__type">
      <t-icon name="file" size="12px" />
      {{ $t(`memory.type.${memory.memory_type}`) }}
    </span>
    <span class="memory-card__age" :class="{ stale: memory.stale_days > 30 }">
      {{ formatTimeAgo(memory.created_at) }}
    </span>
  </div>
</div>
```

### 5.3 Table View (Toggle)

```
┌────┬─────────┬──────────┬──────────────────────────────────┬──────┬──────┬──────────┐
│ ☐  │ Verdict │ Type     │ Content                          │ Tier │ Imp  │ Created  │
├────┼─────────┼──────────┼──────────────────────────────────┼──────┼──────┼──────────┤
│ ☐  │ decision│procedural│ Deploy service mới: build Doc... │  1   │  4⭐ │ 3d ago   │
│ ☐  │ fixed   │semantic  │ Bug X happens when port 8080... │  0   │  5⭐ │ 7d ago   │
│ ☐  │ none    │episodic  │ Hôm qua đã họp về kế hoạch...   │  2   │  2⭐ │ 1d ago   │
│ ☐  │ gotcha  │procedural│ Never run migrations without...  │  1   │  5⭐ │ 1mo ago  │
│ ☐  │ refuted │fact      │ Default port for X is 8080...   │  2   │  0⭐ │ 2mo ago  │
└────┴─────────┴──────────┴──────────────────────────────────┴──────┴──────┴──────────┘
```

### 5.4 Bulk Operations Bar

Chỉ hiển thị khi có ít nhất 1 memory được chọn:

```html
<div v-if="hasSelection" class="memory-bulk-bar">
  <span>{{ selectedIds.size }} selected</span>
  <t-button size="small" @click="bulkSetVerdict('refuted')">Mark Refuted</t-button>
  <t-button size="small" @click="bulkSetVerdict('fixed')">Mark Fixed</t-button>
  <t-button size="small" @click="bulkSetVerdict('none')">Clear Verdict</t-button>
  <t-divider layout="vertical" />
  <t-button size="small" variant="outline" @click="bulkBumpImportance(1)">+1⭐</t-button>
  <t-button size="small" variant="outline" @click="bulkBumpImportance(-1)">-1⭐</t-button>
  <t-divider layout="vertical" />
  <t-button size="small" theme="danger" variant="outline" @click="bulkDelete">Delete</t-button>
</div>
```

### 5.5 Memory Drawer (Detail)

Dùng `t-drawer` hiển thị detail khi click vào memory:

```
┌──────────────────────────────────────────────────────┐
│ ✕  Memory Detail                                     │
│                                                      │
│ Verdict: [decision ▾]    Tier: [1 ▾]    Importance: 4⭐│
│                                                      │
│ ┌──────────────────────────────────────────────────┐ │
│ │ Content                                          │ │
│ │ Quyết định dùng Kubernetes thay vì Docker Swarm  │ │
│ └──────────────────────────────────────────────────┘ │
│                                                      │
│ ┌──────────────┬──────────────┬────────────────────┐ │
│ │ Type         │ Session      │ Hub Score          │ │
│ │ decision     │ sess-abc123  │ 1.2                │ │
│ ├──────────────┼──────────────┼────────────────────┤ │
│ │ Created      │ Last Access  │ Views              │ │
│ │ 2026-06-15   │ 2026-07-08   │ 47                 │ │
│ └──────────────────────────────────────────────────┘ │
│                                                      │
│ Tags: [kubernetes] [deployment] [aws]  [+ Add tag]  │
│                                                      │
│ Relations (6) ───────────────────────────────────── │
│ related_to → "K8s là container orchestration" (0.82)│
│ supports   → "Auto-scaling hoạt động khi CPU >70%"  │
│ justifies  → "AWS EKS có managed control plane"     │
│ [View in Graph →]                                   │
│                                                      │
│ Lint Issues ─────────────────────────────────────── │
│ ⚠ near_duplicate: Content similar to another memory  │
│    [View] [Dismiss]                                  │
│                                                      │
│ History ─────────────────────────────────────────── │
│ 2026-07-08 10:00  Dreamer → bumped importance +1     │
│ 2026-06-20 14:30  User → updated content             │
│ 2026-06-15 09:00  System → created                   │
└──────────────────────────────────────────────────────┘
```

## 6. Sub-Tab 2: Graph

**Component**: `frontend/src/views/memory/MemoryGraph.vue`

### 6.1 Approach

Tái sử dụng custom SVG force-directed graph từ `WikiBrowser.vue`. Memory graph khác wiki graph ở chỗ:

| Aspect | Wiki Graph | Memory Graph |
|--------|-----------|-------------|
| Nodes | Wiki pages | Memory entries |
| Edges | Wiki hierarchy links | Memory relations (supports, contradicts, etc.) |
| Node size | Page importance | `12 + importance * 3` px |
| Node color | Page type | Memory type + verdict overlay |
| Hub indicator | N/A | Ring on hub_score > 1.0 |

### 6.2 Layout

```
┌──────────────────────────────────────────────────────────────┐
│ [Browse] [Graph] [Health ●3] [History]                       │
├──────────────────────────────────────────────────────────────┤
│ Memory Graph — [Depth: 2 ▾]  [Layout: Force ▾]  [Fit] [Exp] │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│              ⬤ ─────────── ⬤                                 │
│             /│\             │\                                │
│      co_   / │ \  supports  │ \  related_to                   │
│     tagged ⬤  │  ⬤          │  ⬤                              │
│             \ │              │                                │
│              \│              │                                │
│               ⬤ (selected)  ⬤─┘                               │
│               ╱│╲                                             │
│              / │ \  justifies                                  │
│             ⬤  │  ⬤                                           │
│                 │                                              │
│                 ⬤                                              │
│                                                              │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ Legend                                                    │ │
│ │ ● decision  ● fact  ● procedural  ● episodic             │ │
│ │ ● semantic  ● preference                                 │ │
│ │ ══ supports  ── contradicts  ── follows                  │ │
│ │ ══ justifies ··· co_tagged  - - related_to               │ │
│ │ ◉ hub (>1.0)  ○ stale (>90d)                            │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ ┌─ Node Detail ───────────────────────────────────────────┐ │
│ │ ✅ fixed · procedural · Tier 1 · 4⭐ · hub 1.2            │ │
│ │ Deploy service mới: build Docker image → push registry   │ │
│ │ → kubectl apply -f deployment.yaml                       │ │
│ │ [Open Detail] [Center Here]                              │ │
│ └──────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

### 6.3 Node Visuals

| Memory Type | Color | Shape | Size | Special |
|-------------|-------|-------|------|---------|
| `decision` | `#0052D9` | Circle | `12 + importance * 3` px | — |
| `fact` | `#2BA471` | Circle | `12 + importance * 3` px | — |
| `procedural` | `#E37318` | Circle | `12 + importance * 3` px | — |
| `episodic` | `#8B8B8B` | Circle | `10 + importance * 3` px | — |
| `semantic` | `#7B5CE7` | Circle | `10 + importance * 3` px | — |
| `preference` | `#E34D59` | Circle | `10 + importance * 3` px | — |
| `hub_score > 1.0` | — | — | — | Ring indicator |
| `stale_days > 90` | — | — | Opacity 0.5 | Dashed border |
| `verdict=refuted` | — | — | Opacity 0.3 | Red X icon |

### 6.4 Edge Visuals

| Relation | Style | Color | Arrow |
|----------|-------|-------|-------|
| `supports` | Solid, 2.5px | `#2BA471` | Yes → |
| `contradicts` | Solid, 2.5px | `#E34D59` | Yes ↔ |
| `follows` | Solid, 1.5px | `#0052D9` | Yes → |
| `justifies` | Dashed, 2px | `#E37318` | Yes → |
| `co_tagged` | Dotted, 1px | `#8B8B8B` | No |
| `related_to` | Dotted, 1px | `#BBBBBB` | No |

### 6.5 Interactions

| Action | Behavior |
|--------|----------|
| Click node | Hiển thị detail panel bên dưới (không navigate) |
| Double-click node | Mở drawer detail |
| Drag node | Reposition (tạm dừng force simulation) |
| Scroll | Zoom in/out |
| Click "Center Here" | Đặt node làm trung tâm, load graph depth-2 từ node đó |
| Click empty space | Deselect |
| Depth selector | `depth=1` (direct) / `depth=2` (2-hop) / `depth=3` (full) |

## 7. Sub-Tab 3: Health

**Component**: `frontend/src/views/memory/MemoryHealth.vue`

### 7.1 Layout (SaiMem-inspired Statistics + Health Checks)

```
┌──────────────────────────────────────────────────────────────┐
│ [Browse] [Graph] [Health ●3] [History]    Last: Today 4:00 AM│
├──────────────────────────────────────────────────────────────┤
│ Overview                                                      │
│ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────────┐ │
│ │ Total  │ │Fixed   │ │Refuted │ │Orphans │ │Stale       │ │
│ │ 12,847 │ │ 347    │ │ 89     │ │ 127    │ │ 43 (>180d) │ │
│ │  100%  │ │ 2.7%   │ │ 0.7%   │ │ 1.0%   │ │ 0.3%       │ │
│ └────────┘ └────────┘ └────────┘ └────────┘ └────────────┘ │
│                                                              │
│ Distribution                          Activity                │
│ ┌────────────────────┐               ┌────────────────────┐  │
│ │ Memory by Type     │               │ Timeline (30 days) │  │
│ │                    │               │ ▄▄▄▄                │  │
│ │ procedural  ████ 35%              │ ████▄▄              │  │
│ │ semantic    ███  28%              │ ██████▄             │  │
│ │ episodic    ██   18%              │ █████████            │  │
│ │ decision    █    10%              │ ██████████▄          │  │
│ │ fact        █     7%              │ ████████████         │  │
│ │ preference  ▏     2%              │ ... per day bar      │  │
│ └────────────────────┘               └────────────────────┘  │
│                                                              │
│ Issues ──────────────────────────────────────────────────── │
│ 🔴 CRITICAL (3)                                               │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ 2 × Contradiction detected                               │ │
│ │   "Use port 8080" vs "Port changed to 9090"              │ │
│ │   [Review in Browse]                                     │ │
│ │ 1 × Duplicate found (fingerprint abc123 ≅ xyz789)        │ │
│ │   [Merge] [Keep Both]                                    │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ 🟡 WARNING (18)                                               │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ 12 × Orphaned (0 tags, 0 relations)                      │ │
│ │ 6  × Stale facts approaching expiry                      │ │
│ │ [View All] [Auto-link Orphans]                           │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ 🟢 INFO (43)                                                  │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ 30 × Low quality (importance ≤ 0, < 50 chars)            │ │
│ │ 8  × WIP memories >30 days (suggest update)              │ │
│ │ 5  × Near-duplicate (cosine >0.90, <0.95)                │ │
│ │ [View All] [Batch Cleanup]                               │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ [Run Dreamer] [Export Report (JSON)]                         │
└──────────────────────────────────────────────────────────────┘
```

## 8. Sub-Tab 4: History

**Component**: `frontend/src/views/memory/MemoryHistory.vue`

### 8.1 Layout (SaiMem-inspired Activity Feed)

```
┌──────────────────────────────────────────────────────────────┐
│ [Browse] [Graph] [Health ●3] [History]   Filter: [All ▾]     │
├──────────────────────────────────────────────────────────────┤
│ Today                                                        │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ 10:00 AM  🤖 Dreamer — merged 2 memories                │ │
│ │           "Deploy service X" + "Deploy X lên production" │ │
│ │            → new merged memory, confidence 0.85          │ │
│ │                                                          │ │
│ │ 10:00 AM  🤖 Dreamer — updated verdict: none → refuted  │ │
│ │           "Port mặc định của service X là 8080"          │ │
│ │            → contradicted by newer memory, confidence 0.90│ │
│ │                                                          │ │
│ │ 09:30 AM  👤 User — created "Cấu hình CI/CD mới..."     │ │
│ │                                                          │ │
│ │ 09:15 AM  🤖 System — auto-linked 3 relations           │ │
│ │           episodic#abc → related_to → procedural#def     │ │
│ │           procedural#ghi → supports → decision#jkl       │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ Yesterday                                                    │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ 03:00 PM  🧹 Pruner — deleted 12 tier-3 memories         │ │
│ │           (soft-deleted >14d, 0 access count)            │ │
│ │                                                          │ │
│ │ 11:00 AM  👤 User — bulk updated 5 memories → fixed     │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ June 15                                                      │
│ ┌──────────────────────────────────────────────────────────┐ │
│ │ 09:00 AM  🔧 System — Health Check complete              │ │
│ │           3 critical, 18 warning, 43 info issues found   │ │
│ └──────────────────────────────────────────────────────────┘ │
│                                                              │
│ ← Load More                                                  │
└──────────────────────────────────────────────────────────────┘
```

### 8.2 Event Types & Icons

| Event | Icon | Color |
|-------|------|-------|
| `memory:created` | `➕` / `add-circle` | Green |
| `memory:updated` | `✏️` / `edit` | Blue |
| `memory:deleted` | `🗑️` / `delete` | Red |
| `verdict:changed` | `🏷️` / `flag` | Orange |
| `dreamer:action` | `🤖` / `logo-github` | Purple |
| `consolidation:merge` | `🔗` / `link` | Teal |
| `consolidation:decay` | `⏳` / `time` | Gray |
| `pruner:delete` | `🧹` / `clear` | Gray |
| `health:check` | `🔧` / `tools` | Blue |
| `import:batch` | `📥` / `file-import` | Blue |
| `export:batch` | `📤` / `file-export` | Blue |
| `auto_link` | `🔍` / `search` | Teal |

### 8.3 Filter

```html
<div class="history-filters">
  <t-select v-model="historyFilter.eventType" :options="eventTypeOptions" 
            placeholder="All Events" clearable size="small" multiple />
  <t-date-range-picker v-model="historyFilter.dateRange" size="small" />
</div>
```

## 9. Integration vào KnowledgeBase.vue

### 9.1 Changes Required

| Line area | Change |
|-----------|--------|
| `validTabs` | Thêm `'memory'` |
| `activeKbTab` type | `KbTab` auto-extends |
| Breadcrumb | Thêm `<span>` cho Memory tab |
| Template body | Thêm `<template v-if="activeKbTab === 'memory'">` block |
| Script imports | Import memory components + store |
| Component registration | Register MemoryBrowse, MemoryGraph, MemoryHealth, MemoryHistory |

### 9.2 Template Integration (Precise Location)

```html
<!-- KnowledgeBase.vue — after Wiki/Graph block (~line 2081) -->
</div>  <!-- end of wiki-main-area -->

<!-- Memory tab — NEW -->
<template v-if="activeKbTab === 'memory'">
  <div class="memory-main-area">
    <div class="memory-subtabs">
      <span v-for="sub in memorySubTabs" :key="sub.key"
            :class="{ active: memoryStore.activeSubTab === sub.key }"
            @click="memoryStore.setSubTab(sub.key)">
        <t-icon :name="sub.icon" size="16px" />
        {{ sub.label }}
        <t-badge v-if="sub.badge && sub.badge > 0" :count="sub.badge" size="small" />
      </span>
      <div class="memory-subtab-actions">
        <t-button v-if="memoryStore.activeSubTab === 'browse'"
                  variant="text" size="small"
                  @click="memoryStore.toggleViewMode()">
          <t-icon :name="memoryStore.viewMode === 'grid' ? 'view-list' : 'view-module'" />
        </t-button>
      </div>
    </div>
    <KeepAlive>
      <MemoryBrowse  v-if="memoryStore.activeSubTab === 'browse'"  :kb-id="kbId" />
      <MemoryGraph   v-if="memoryStore.activeSubTab === 'graph'"   :kb-id="kbId" />
      <MemoryHealth  v-if="memoryStore.activeSubTab === 'health'"  :kb-id="kbId" />
      <MemoryHistory v-if="memoryStore.activeSubTab === 'history'" :kb-id="kbId" />
    </KeepAlive>
  </div>
</template>
```

### 9.3 Memory-Only KB Mode

Nếu KB được tạo với type "memory-only" (không có documents/wiki), giao diện tự động chuyển tab mặc định thành Memory:

```typescript
// KnowledgeBase.vue
watch(kbInfo, (info) => {
  if (info?.type === 'memory') {
    activeKbTab.value = 'memory'
  }
})
```

Chỉ hiển thị tab Memory (bỏ Documents/Wiki/Graph).

## 10. Settings Integration

### 10.1 Memory Backend Toggle

```diff
// frontend/src/views/settings/GeneralSettings.vue

- <t-switch :value="isMemoryEnabled" :disabled="!isNeo4jAvailable || memorySaving"
+ <t-switch :value="isMemoryEnabled" :disabled="!memoryBackendAvailable || memorySaving"
    @change="handleMemoryChange" />

- <div v-if="!isNeo4jAvailable" class="warning-banner">{{ $t('memoryRequiresNeo4j') }}</div>
+ <div v-if="!memoryBackendAvailable" class="warning-banner">{{ $t('memoryNotAvailable') }}</div>
+ <div v-else class="memory-backend-info">
+   {{ memoryStatus.backend === 'v2' ? 'PostgreSQL (v2)' : 'Neo4j (v1)' }}
+   · {{ memoryStatus.memory_count }} memories
+ </div>
```

### 10.2 API: Memory Status

```json
// GET /api/v1/tenants/memory-status
{
  "backend": "v2",
  "available": true,
  "neo4j_available": false,
  "memory_count": 12847
}
```

## 11. Chat Memory Context Panel

### 11.1 Expandable Context in Chat

Trong `botmsg.vue`, thêm expandable panel cho memory context:

```
┌──────────────────────────────────────────────────────────────┐
│ User: cách deploy service mới lên production?                │
├──────────────────────────────────────────────────────────────┤
│ [🧠 Memory Context · 3 results · 450/2000 tokens] [▼]       │
├──────────────────────────────────────────────────────────────┤
│ Assistant: Dựa trên memory context, để deploy service mới...│
└──────────────────────────────────────────────────────────────┘
```

Khi expand:

```
┌──────────────────────────────────────────────────────────────┐
│ 🧠 Memory Context · 3 results · 450/2000 tokens · Full mode  │
├──────────────────────────────────────────────────────────────┤
│ ✅ fixed  · procedural · T1 · 87% · 3d ago · hub 1.2         │
│ Deploy service mới: build Docker image → push registry       │
│ → kubectl apply -f deployment.yaml                           │
│                                                              │
│ ⚠ decision · decision   · T1 · 72% · 45d ago · hub 0.8       │
│ Quyết định dùng Kubernetes thay vì Docker Swarm              │
│                                                              │
│ · none    · semantic   · T2 · 55% · 10d ago · hub 0.3       │
│ Production cluster chạy trên EKS với 3 node groups           │
└──────────────────────────────────────────────────────────────┘
```

## 12. Responsive Behavior

| Breakpoint | KB Tabs | Sub-Tabs | Browse Cards |
|------------|---------|----------|-------------|
| >1440px | Inline breadcrumb | Inline | 4 cols |
| 1024-1440px | Inline breadcrumb | Inline | 3 cols |
| 768-1024px | Scrollable row | Scrollable row | 2 cols |
| <768px | Dropdown menu | Dropdown menu | 1 col |

## 13. i18n Keys

```typescript
// Add to all 5 locale files under existing structure

{
  knowledgeBase: {
    tabs: {
      memory: 'Memory',  // thêm vào tab labels
    },
  },

  memory: {
    // Sub-tabs
    subTabs: {
      browse: 'Browse',
      graph: 'Graph',
      health: 'Health',
      history: 'History',
    },

    // Verdict
    verdict: {
      none: 'None',
      fixed: 'Fixed',
      refuted: 'Refuted',
      decision: 'Decision',
      gotcha: 'Gotcha',
      wip: 'WIP',
    },

    // Types
    type: {
      episodic: 'Episodic',
      semantic: 'Semantic',
      procedural: 'Procedural',
      decision: 'Decision',
      preference: 'Preference',
      fact: 'Fact',
    },

    // Browse
    browse: {
      search: 'Search memories...',
      noResults: 'No memories found',
      selected: '{count} selected',
      gridView: 'Grid view',
      tableView: 'Table view',
    },

    // Bulk actions
    bulk: {
      markRefuted: 'Mark Refuted',
      markFixed: 'Mark Fixed',
      clearVerdict: 'Clear Verdict',
      bumpImportance: '{delta} importance',
      deleteSelected: 'Delete Selected',
    },

    // Detail drawer
    detail: {
      title: 'Memory Detail',
      content: 'Content',
      metadata: 'Metadata',
      relations: 'Relations',
      lintIssues: 'Lint Issues',
      history: 'History',
      addTag: 'Add tag',
      viewInGraph: 'View in Graph',
      noRelations: 'No relations yet',
      noLintIssues: 'No lint issues',
      staleWarning: 'This memory may be outdated ({days} days old)',
      protectedVerdict: 'Protected verdict — manual change only',
    },

    // Graph
    graph: {
      depth: 'Depth',
      layout: 'Layout',
      fitToScreen: 'Fit to Screen',
      exportSvg: 'Export SVG',
      legend: 'Legend',
      centerHere: 'Center Here',
      openDetail: 'Open Detail',
    },

    // Health
    health: {
      overview: 'Overview',
      total: 'Total',
      distribution: 'Distribution',
      activity: 'Activity (30 days)',
      issues: 'Issues',
      lastCheck: 'Last check: {time}',
      runDreamer: 'Run Dreamer',
      exportReport: 'Export Report (JSON)',

      severity: {
        critical: 'CRITICAL',
        warning: 'WARNING',
        info: 'INFO',
      },

      metricLabels: {
        total: 'Total',
        fixed: 'Fixed',
        refuted: 'Refuted', 
        orphans: 'Orphans',
        stale: 'Stale',
      },

      actions: {
        review: 'Review',
        merge: 'Merge',
        keepBoth: 'Keep Both',
        dismiss: 'Dismiss',
        viewAll: 'View All',
        autoLink: 'Auto-link',
        batchCleanup: 'Batch Cleanup',
      },
    },

    // History
    history: {
      filterByType: 'Filter by event type',
      filterByDate: 'Filter by date',
      loadMore: 'Load More',
      today: 'Today',
      yesterday: 'Yesterday',

      events: {
        'memory:created': 'created memory',
        'memory:updated': 'updated memory',
        'memory:deleted': 'deleted memory',
        'verdict:changed': 'changed verdict',
        'dreamer:action': 'Dreamer action',
        'consolidation:merge': 'merged memories',
        'consolidation:decay': 'decayed importance',
        'pruner:delete': 'deleted expired',
        'health:check': 'Health Check complete',
        'import:batch': 'imported batch',
        'export:batch': 'exported batch',
        'auto_link': 'auto-linked relations',
      },
    },

    // Chat context
    chatContext: {
      label: 'Memory Context',
      results: '{count} results',
      tokens: '{used}/{total} tokens',
      mode: '{mode} mode',
      expand: 'Expand',
      collapse: 'Collapse',
      overflow: 'Memory context near limit ({used}/{total} tokens)',
      truncated: 'Some results were truncated to fit budget.',
    },
  },

  // Settings
  memoryNotAvailable: 'Memory backend is not available. Check your database configuration.',
}
```

## 14. Files to Create & Modify

| Action | File | Notes |
|--------|------|-------|
| **Create** | `frontend/src/views/memory/MemoryBrowse.vue` | Card grid + table + filters + bulk |
| **Create** | `frontend/src/views/memory/MemoryCard.vue` | Reusable card component |
| **Create** | `frontend/src/views/memory/MemoryTable.vue` | Reusable table component |
| **Create** | `frontend/src/views/memory/MemoryGraph.vue` | SVG force-directed graph |
| **Create** | `frontend/src/views/memory/MemoryHealth.vue` | Dashboard + issue list |
| **Create** | `frontend/src/views/memory/MemoryHistory.vue` | Activity timeline |
| **Create** | `frontend/src/views/memory/MemoryDrawer.vue` | Detail drawer |
| **Create** | `frontend/src/api/memory/index.ts` | API module |
| **Create** | `frontend/src/stores/memory.ts` | Pinia store |
| **Modify** | `frontend/src/views/knowledge/KnowledgeBase.vue` | +1 tab, +KeepAlive block, +imports |
| **Modify** | `frontend/src/views/settings/GeneralSettings.vue` | ~3 lines (Neo4j → backend) |
| **Modify** | `frontend/src/i18n/locales/*.ts` (×5) | +80 keys each |
| **Modify** | `frontend/src/views/chat/components/botmsg.vue` | Memory context expand panel |

**Tổng**: 8 file mới, 4 file sửa. Không thay đổi router, không thêm sidebar item.
