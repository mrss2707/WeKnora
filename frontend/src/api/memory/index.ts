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

export interface CreateMemoryRequest {
  kb_id: string
  content: string
  memory_type?: string
  importance?: number
  tier?: number
  session_id?: string
  tags?: string[]
}

export interface UpdateMemoryRequest {
  content?: string
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
}

export interface GraphEdge {
  source: string
  target: string
  relation_type: string
  weight: number
}

export interface GraphData {
  nodes: GraphNode[]
  edges: GraphEdge[]
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
}

export interface TriggerDreamRequest {
  kb_id: string
  dry_run?: boolean
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
  cache_warmer: CacheWarmerConfig
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

export interface CacheWarmerConfig {
  enabled: boolean
  top_queries_n: number
  refresh_interval: string
}

export interface LintOnWriteConfig {
  enabled: boolean
  stale_threshold_days: number
  contradiction_threshold: number
  near_duplicate_threshold: number
}

// -------------------------------------------------------------------------
// Timeline / History Event Types
// -------------------------------------------------------------------------

export type TimelineEventType =
  | 'created'
  | 'updated'
  | 'deleted'
  | 'verdict_changed'
  | 'dreamer_action'
  | 'consolidation'
  | 'pruner'
  | 'health_check'

export interface TimelineEvent {
  id: string
  type: TimelineEventType
  timestamp: string
  description: string
  memory_id?: string
  memory_content_preview?: string
  metadata?: Record<string, unknown>
}

// -------------------------------------------------------------------------
// Paginated Response
// -------------------------------------------------------------------------

export interface PaginatedMemoriesResponse {
  data: AgentMemory[]
  total: number
  page: number
  page_size: number
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
  limit?: number
}) {
  const query = new URLSearchParams()
  if (params.kb_id) query.set('kb_id', params.kb_id)
  query.set('q', params.q)
  if (params.type) query.set('type', params.type)
  if (params.limit != null) query.set('limit', String(params.limit))
  return get<{ data: MemorySearchResult[] }>(`/api/v1/memories/search?${query.toString()}`)
}

/** Get the memory graph centred on a specific memory. */
export function getMemoryGraph(id: string, params?: { kb_id?: string; deep?: boolean }) {
  const query = new URLSearchParams()
  if (params?.kb_id) query.set('kb_id', params.kb_id)
  if (params?.deep != null) query.set('deep', String(params.deep))
  const qs = query.toString()
  return get<{ data: GraphData }>(`/api/v1/memories/graph/${id}${qs ? '?' + qs : ''}`)
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

/** Trigger the dreamer worker. */
export function triggerDream(data: TriggerDreamRequest) {
  return post<{ data: DreamResult }>('/api/v1/memories/dream', data)
}

/** Get the memory backend status for the current tenant. */
export function getMemoryStatus() {
  return get<{ data: MemoryStatus }>('/api/v1/tenants/memory-status')
}
