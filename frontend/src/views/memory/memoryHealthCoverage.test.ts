import assert from 'node:assert/strict'
import test from 'node:test'
import type { HealthReport } from '@/api/memory/index'
import { healthCoverageState } from './memoryHealthCoverage'

function report(patch: Partial<HealthReport> = {}): HealthReport {
  return { tenant_id: 'test', checked_at: '', total_issues: 0, by_severity: {}, issues: [], ...patch }
}

test('zero findings with missing coverage never implies a complete assessment', () => {
  assert.equal(healthCoverageState(report()).partial, true)
  assert.deepEqual(healthCoverageState(report()).reasons, ['coverage_unknown'])
})

test('complete scan still warns about unavailable and unsupported checks', () => {
  const state = healthCoverageState(report({
    coverage: { total: 20, scanned: 20, limit: 1000, complete: true, reasons: [] },
    checks: {
      embedding_dimension: { status: 'unavailable', reason: 'model_unavailable', examined: 0 },
      contradiction: { status: 'partial', reason: 'unsupported_assertions', examined: 10 },
    },
  }))
  assert.equal(state.partial, true)
  assert.deepEqual(state.checks.map(c => c.issueType), ['embedding_dimension_mismatch', 'contradiction'])
})

test('unknown server check/reason uses existing localized fallback categories', () => {
  const state = healthCoverageState(report({
    coverage: { total: 2000, scanned: 1000, limit: 1000, complete: false, reasons: ['future_limit'] },
    checks: { future_check: { status: 'unavailable', reason: 'future_error', examined: 0 } },
  }))
  assert.equal(state.partial, true)
  assert.equal(state.checks[0].issueType, 'semantic')
  assert.equal(state.checks[0].reason, 'unknown')
  assert.deepEqual(state.reasons, ['coverage_unknown'])
})

test('completed checks and complete scan are reported without partial warning', () => {
  const state = healthCoverageState(report({
    coverage: { total: 20, scanned: 20, limit: 1000, complete: true, reasons: [] },
    checks: { contradiction: { status: 'completed', examined: 20 } },
  }))
  assert.equal(state.partial, false)
  assert.deepEqual(state.checks, [])
})
