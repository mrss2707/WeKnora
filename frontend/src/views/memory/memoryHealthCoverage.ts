import type { HealthReport } from '@/api/memory/index'

const checkTypes: Record<string, string> = {
  orphans: 'orphan', stale: 'stale', duplication: 'duplication',
  graph_fragmentation: 'graph_fragmentation', verdict_consistency: 'verdict_consistency',
  contradiction: 'contradiction', embedding_dimension: 'embedding_dimension_mismatch',
  dreamer_model: 'dreamer_model_unavailable',
}
const checkReasons = new Set(['unsupported_assertions', 'model_unavailable', 'embedding_check_failed', 'no_embeddings'])
const coverageReasons = new Set(['scan_limit', 'issue_limit', 'filtered_results', 'coverage_unknown', 'inconsistent_count'])

export function healthCoverageState(report: HealthReport | null) {
  const checks = Object.entries(report?.checks ?? {})
    .filter(([, check]) => check.status !== 'completed')
    .map(([name, check]) => ({
      name, issueType: checkTypes[name] ?? 'semantic',
      reason: checkReasons.has(check.reason ?? '') ? check.reason! : 'unknown',
      examined: check.examined,
    }))
  const coverage = report?.coverage
  return {
    partial: !coverage?.complete || Object.keys(report?.checks ?? {}).length === 0 || checks.length > 0,
    checks,
    reasons: (coverage?.reasons ?? ['coverage_unknown']).map(reason => coverageReasons.has(reason) ? reason : 'coverage_unknown'),
  }
}
