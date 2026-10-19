import { get, post, put, del } from '@/utils/request'

// -------------------------------------------------------------------------
// Enums / Constants
// -------------------------------------------------------------------------

export type MemoryVerdict = 'none' | 'fixed' | 'refuted' | 'decision' | 'gotcha' | 'wip'

export const MEMORY_VERDICTS: MemoryVerdict[] = ['none', 'fixed', 'refuted', 'decision', 'gotcha', 'wip']

export function isVerdictProtected(verdict: MemoryVerdict): boolean {
  return verdict === 'decision' || verdict === 'fixed'
}

export type MemoryType = 'semantic' | 'episodic' | 'procedural' | string

// -------------------------------------------------------------------------
// Domain Types
// -------------------------------------------------------------------------

export interface AgentMemory {
  id: string
  tenant_id: string
  kb_id: string
  user_id: string
  content: string
  memory_type: string
  importance: number
  tier: number
  verdict: MemoryVerdict
  hub_score: number
  access_count: number
  session_id: string
  fingerprint?: string
  tags?: string[]
  metadata?: Record<string, unknown>
  last_accessed_at?: string
  expires_at?: string
  created_at: string
  updated_at: string
}

/** Content length bounds enforced by the backend (characters). */
export const MEMORY_CONTENT_MIN = 10
export const MEMORY_CONTENT_MAX = 10000

/** Importance range enforced by the backend (PUT returns 400 outside it). */
export const MEMORY_IMPORTANCE_MIN = -5
export const MEMORY_IMPORTANCE_MAX = 6

/** Clamp an importance value to the backend range. */
export function clampImportance(value: number): number {
  return Math.max(MEMORY_IMPORTANCE_MIN, Math.min(MEMORY_IMPORTANCE_MAX, Math.round(value)))
}

/** Server-owned fields (author, type, importance, tier, tags) are derived by ingestion. */
export interface CreateMemoryRequest {
  kb_id: string
  content: string
  session_id?: string
}

/**
 * Partial update. Send the memory's `updated_at` as `expected_updated_at` to
 * get a 409 instead of overwriting someone else's change.
 */
export interface UpdateMemoryRequest {
  content?: string
  expected_updated_at?: string
  memory_type?: string
  importance?: number
  tier?: number
  verdict?: MemoryVerdict
  tags?: string[]
}

export interface MemoryRelation {
  id: string
  tenant_id: string
  from_uuid: string
  to_uuid: string
  relation_type: string
  weight: number
  created_at: string
}

// -------------------------------------------------------------------------
// Filter / Search
// -------------------------------------------------------------------------

export interface MemoryFilter {
  tenant_id?: string
  user_id?: string
  query?: string
  memory_type?: string
  tier?: number | null
  verdicts?: MemoryVerdict[]
  session_id?: string
  min_score?: number
  deep_graph?: boolean
  limit?: number
  offset?: number
}

export interface MemorySearchResult {
  memory: AgentMemory
  score: number
  is_stale: boolean
  stale_days: number
}

export interface MemoryListParams {
  kb_id?: string
  page?: number
  page_size?: number
  type?: string
  verdict?: string
  tier?: number
  keyword?: string
  /** "me" lists only the caller's memories. */
  author?: string
}

// -------------------------------------------------------------------------
// Lint / Save Result
// -------------------------------------------------------------------------

export interface MemoryLintIssue {
  rule: string
  severity: string // warning | error | info
  message: string
  source_id?: string
}

export interface SaveMemoryResult {
  memory: AgentMemory
  created: boolean
  lint_issues: MemoryLintIssue[]
}

// -------------------------------------------------------------------------
// Graph
// -------------------------------------------------------------------------

export interface GraphNode {
  id: string
  label: string
  type: string
  importance: number
  verdict: MemoryVerdict
  hub_score: number
  is_stale?: boolean
  /** True for the memory the graph is centred on. */
  focal?: boolean
}

/** Relation kinds the backend creates. */
export type MemoryRelationType = 'related_to' | 'justifies' | 'contradicts'

export interface GraphEdge {
  id?: string
  source: string
  target: string
  relation_type: MemoryRelationType | string
  weight: number
}

export interface GraphData {
  nodes: GraphNode[]
  edges: GraphEdge[]
  /** True when node/edge limits cut the graph short. */
  truncated?: boolean
}

// -------------------------------------------------------------------------
// Statistics
// -------------------------------------------------------------------------

export interface MemoryStats {
  total_memories: number
  by_type: Record<string, number>
  by_tier: Record<string, number>
  by_verdict: Record<string, number>
  avg_importance: number
  avg_hub_score: number
}

// -------------------------------------------------------------------------
// Health
// -------------------------------------------------------------------------

export interface MemoryHealthIssue {
  type: string
  memory_id: string
  description: string
  severity: string // low | medium | high | critical
  suggestion: string
}

export interface HealthReport {
  tenant_id: string
  checked_at: string
  total_issues: number
  by_severity: Record<string, number>
  issues: MemoryHealthIssue[]
  coverage?: { total: number; scanned: number; limit: number; complete: boolean; reasons: string[] }
  checks?: Record<string, { status: 'completed' | 'partial' | 'unavailable'; reason?: string; examined: number }>
}

// -------------------------------------------------------------------------
// Dreamer
// -------------------------------------------------------------------------

export interface DreamAction {
  type: string
  target_id: string
  target_ids?: string[]
  new_verdict?: string
  delta?: number
  reason: string
  confidence: number
}

export interface DreamResult {
  actions_proposed: number
  actions_applied: number
  actions: DreamAction[]
  token_used: number
  /** Set when the pass applied changes; undo it with undoDreamRun. */
  run_id?: string
}

export interface TriggerDreamRequest {
  kb_id: string
  /** Preview: return proposals without applying or recording anything. */
  dry_run?: boolean
}

export interface DreamStatus {
  enabled: boolean
  /** Background passes run (dreamer and worker kill switch both on). */
  scheduled: boolean
  dry_run: boolean
  interval: string
  runs: number
  last_run_at?: string
  running: boolean
  /** Merge probation is over: merges are applied, not only proposed. */
  merge_live: boolean
  merge_live_after_runs: number
  last_stats?: Record<string, unknown>
}

// -------------------------------------------------------------------------
// Audit history
// -------------------------------------------------------------------------

export type MemoryAuditAction =
  | 'created'
  | 'patched'
  | 'merged'
  | 'merged_into'
  | 'deleted'
  | 'unmerged'
  | 'dreamed'
  | 'reclaimed'

export const MEMORY_AUDIT_ACTIONS: MemoryAuditAction[] = [
  'created',
  'patched',
  'merged',
  'merged_into',
  'deleted',
  'unmerged',
  'reclaimed',
]

/** The audited state of a memory (no embedding). */
export interface MemoryAuditImage {
  id: string
  kb_id: string
  user_id?: string
  content: string
  memory_type: string
  importance: number
  tier: number
  verdict: MemoryVerdict
  tags: string[] | null
}

export interface MemoryAuditEntry {
  id: number
  tenant_id: string
  kb_id: string
  memory_id: string
  /** "user", a worker ("dreamer", "pruner", ...) or a machine principal. */
  actor: string
  user_id?: string
  action: MemoryAuditAction | string
  before?: MemoryAuditImage
  after?: MemoryAuditImage
  details?: Record<string, unknown>
  created_at: string
}

export interface MemoryHistoryParams {
  kb_id?: string
  memory_id?: string
  action?: string
  page?: number
  page_size?: number
}

export interface PaginatedHistoryResponse {
  success?: boolean
  data: {
    items: MemoryAuditEntry[]
    total: number
    page: number
    page_size: number
  }
}

// -------------------------------------------------------------------------
// Memory Status
// -------------------------------------------------------------------------

export interface MemoryStatus {
  backend: string
  available: boolean
  memory_count: number
}

// -------------------------------------------------------------------------
// Token Budget
// -------------------------------------------------------------------------

export interface TokenBudgetConfig {
  max_total_tokens: number
  truncate_threshold: number
  summary_threshold: number
  max_per_memory: number
}

export interface TokenBudgetInfo {
  mode: string // full | truncated | summary
  used: number
  remaining: number
}

// -------------------------------------------------------------------------
// Config Types
// -------------------------------------------------------------------------

export interface DreamerConfig {
  enabled: boolean
  interval: string
  /** Deprecated, ignored by the server: the Dreamer model is chosen per knowledge base. */
  model_id: string
  max_actions: number
  token_budget: number
  dry_run: boolean
}

export interface MemoryV2Config {
  enabled: boolean
  max_search_results: number
  min_score_threshold: number
  semantic_dedup: DedupConfig
  recency_boost: RecencyBoostConfig
  token_budget: TokenBudgetConfig
  dreamer: DreamerConfig
  lint_on_write: LintOnWriteConfig
  hnsw_m: number
  hnsw_ef_construction: number
  hnsw_ef_search: number
}

export interface DedupConfig {
  exact_threshold: number
  near_threshold: number
  max_merges: number
  merge_max_chars: number
}

export interface RecencyBoostConfig {
  enabled: boolean
  short_term_multiplier: number
  short_term_window: string
  long_term_factor: number
  long_term_half_life: number
}

export interface LintOnWriteConfig {
  enabled: boolean
  stale_threshold_days: number
  contradiction_threshold: number
  near_duplicate_threshold: number
}

// -------------------------------------------------------------------------
// Paginated Response
// -------------------------------------------------------------------------

export interface PaginatedMemoriesResponse {
  success?: boolean
  data: {
    items: AgentMemory[]
    total: number
    page: number
    page_size: number
  }
}

// -------------------------------------------------------------------------
// API Functions
// -------------------------------------------------------------------------

/** List memories with optional filters and pagination. */
export function listMemories(params?: MemoryListParams) {
  const query = new URLSearchParams()
  if (params?.kb_id) query.set('kb_id', params.kb_id)
  if (params?.page != null) query.set('page', String(params.page))
  if (params?.page_size != null) query.set('page_size', String(params.page_size))
  if (params?.type) query.set('type', params.type)
  if (params?.verdict) query.set('verdict', params.verdict)
  if (params?.tier != null) query.set('tier', String(params.tier))
  if (params?.keyword) query.set('keyword', params.keyword)
  if (params?.author) query.set('author', params.author)
  const qs = query.toString()
  return get<PaginatedMemoriesResponse>(`/api/v1/memories${qs ? '?' + qs : ''}`)
}

/** Get a single memory by ID. */
export function getMemory(id: string) {
  return get<{ data: AgentMemory }>(`/api/v1/memories/${id}`)
}

/** Create a new memory. */
export function createMemory(data: CreateMemoryRequest) {
  return post<{ data: SaveMemoryResult }>('/api/v1/memories', data)
}

/** Update an existing memory. */
export function updateMemory(id: string, data: UpdateMemoryRequest) {
  return put<{ data: AgentMemory }>(`/api/v1/memories/${id}`, data)
}

/** Delete a memory. */
export function deleteMemory(id: string) {
  return del<{ success: boolean }>(`/api/v1/memories/${id}`)
}

/** Search memories by query text. */
export function searchMemories(params: {
  kb_id?: string
  q: string
  type?: string
  verdict?: string
  tier?: number
  author?: string
  limit?: number
}) {
  const query = new URLSearchParams()
  if (params.kb_id) query.set('kb_id', params.kb_id)
  query.set('q', params.q)
  if (params.type) query.set('type', params.type)
  if (params.verdict) query.set('verdict', params.verdict)
  if (params.author) query.set('author', params.author)
  if (params.tier != null) query.set('tier', String(params.tier))
  if (params.limit != null) query.set('limit', String(params.limit))
  return get<{ data: MemorySearchResult[] }>(`/api/v1/memories/search?${query.toString()}`)
}

/** Get the memory graph centred on a specific memory, `depth` hops deep (1..3). */
export function getMemoryGraph(id: string, params?: { depth?: number; limit?: number }) {
  const query = new URLSearchParams()
  if (params?.depth) query.set('depth', String(params.depth))
  if (params?.limit) query.set('limit', String(params.limit))
  const qs = query.toString()
  return get<{ data: GraphData }>(`/api/v1/memories/graph/${encodeURIComponent(id)}${qs ? '?' + qs : ''}`)
}

/** Get a knowledge base's graph overview: its most connected memories and their relations. */
export function getMemoryGraphOverview(kbId: string, limit = 30) {
  const query = new URLSearchParams({ kb_id: kbId, limit: String(limit) })
  return get<{ data: GraphData }>(`/api/v1/memories/graph?${query.toString()}`)
}

/** Rebuild a knowledge base's automatic relations (Admin). */
export function relinkMemories(kbId: string) {
  return post<{ data: { memories: number; relations: number } }>('/api/v1/memories/relink', { kb_id: kbId })
}

/** Get memory statistics for a knowledge base. */
export function getMemoryStats(kbId?: string) {
  const qs = kbId ? `?kb_id=${kbId}` : ''
  return get<{ data: MemoryStats }>(`/api/v1/memories/stats${qs}`)
}

/** Get the health report for memories. */
export function getHealthReport(kbId?: string) {
  const qs = kbId ? `?kb_id=${kbId}` : ''
  return get<{ data: HealthReport }>(`/api/v1/memories/health${qs}`)
}

/**
 * A dream pass waits for one chat-model call, which the server bounds at five
 * minutes; the 30s JSON default would report a failure for a pass that still
 * completes (slow hosted or reasoning models on a full batch).
 */
export const DREAM_REQUEST_TIMEOUT_MS = 6 * 60 * 1000

/** Trigger the dreamer worker. */
export function triggerDream(data: TriggerDreamRequest) {
  return post<{ data: DreamResult }>('/api/v1/memories/dream', data, { timeout: DREAM_REQUEST_TIMEOUT_MS })
}

export interface DreamUndoResult {
  run_id: string
  reverted: number
  /** Changes left in place: changed_after_pass, already_undone, not_reversible or failed. */
  skipped: { memory_id: string; action: string; reason: string }[]
}

/** Revert the changes of one Dreamer pass (Admin); memories edited since are left alone. */
export function undoDreamRun(kbId: string, runId: string) {
  return post<{ data: DreamUndoResult }>('/api/v1/memories/dream/undo', { kb_id: kbId, run_id: runId })
}

/** Get a knowledge base's dreamer settings and last run. */
export function getDreamStatus(kbId: string) {
  return get<{ data: DreamStatus }>(`/api/v1/memories/dream?kb_id=${encodeURIComponent(kbId)}`)
}

/** Where a Memory V2 task's model comes from. */
export type MemoryModelSource = 'setting' | 'kb_summary' | 'tenant_default' | 'unavailable'

export interface MemoryEffectiveModel {
  model_id: string
  name: string
  source: MemoryModelSource
}

/** Per-knowledge-base model choices for the Dreamer and Memory V2 text tasks. */
export interface MemoryModelSettings {
  kb_id: string
  dreamer_model_id: string
  text_model_id: string
  updated_at?: string
  dreamer: MemoryEffectiveModel
  text: MemoryEffectiveModel
}

/** Get a knowledge base's Memory V2 model settings. */
export function getMemoryModelSettings(kbId: string) {
  return get<{ data: MemoryModelSettings }>(`/api/v1/memories/settings?kb_id=${encodeURIComponent(kbId)}`)
}

/** Replace a knowledge base's Memory V2 model choices (Admin); '' restores the summary model. */
export function updateMemoryModelSettings(data: { kb_id: string; dreamer_model_id: string; text_model_id: string }) {
  return put<{ data: MemoryModelSettings }>('/api/v1/memories/settings', data)
}

/** List the audit trail of a knowledge base or of one memory. */
export function getMemoryHistory(params: MemoryHistoryParams) {
  const query = new URLSearchParams()
  if (params.kb_id) query.set('kb_id', params.kb_id)
  if (params.memory_id) query.set('memory_id', params.memory_id)
  if (params.action) query.set('action', params.action)
  if (params.page != null) query.set('page', String(params.page))
  if (params.page_size != null) query.set('page_size', String(params.page_size))
  return get<PaginatedHistoryResponse>(`/api/v1/memories/history?${query.toString()}`)
}

/** Reverse the latest merge into a memory (Admin). */
export function undoMemoryMerge(id: string) {
  return post<{ data: AgentMemory }>(`/api/v1/memories/${encodeURIComponent(id)}/undo-merge`, {})
}

/** Get the memory backend status for the current tenant. */
export function getMemoryStatus() {
  return get<{ data: MemoryStatus }>('/api/v1/tenants/memory-status')
}
