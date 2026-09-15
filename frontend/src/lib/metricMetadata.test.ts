import { describe, expect, it } from 'vitest'
import {
  getMetricMetadata,
  getMetricsBySource,
  getServiceDisplayName,
  getSourceDisplayName,
} from './metricMetadata'

describe('OpenCode metric metadata', () => {
  it('formats OpenCode token and cost metrics with the correct source and units', () => {
    const tokens = getMetricMetadata('opencode.token.usage')
    expect(tokens.source).toBe('opencode')
    expect(tokens.unit.formatter).toBe('tokens')

    const cost = getMetricMetadata('opencode.cost.usage')
    expect(cost.source).toBe('opencode')
    expect(cost.unit.formatter).toBe('currency')
  })

  it('infers unknown OpenCode metrics from the opencode prefix', () => {
    const metadata = getMetricMetadata('opencode.custom.metric')
    expect(metadata.source).toBe('opencode')
    expect(metadata.displayName).toBe('Custom Metric')

    const unknownCost = getMetricMetadata('opencode.llm.cost')
    expect(unknownCost.unit.formatter).toBe('currency')
  })

  it('groups and displays OpenCode as a provider', () => {
    expect(getSourceDisplayName('opencode')).toBe('OpenCode')
    expect(getServiceDisplayName('opencode')).toBe('OpenCode')
    expect(getMetricsBySource().opencode.map((metric) => metric.name)).toContain('opencode.cost.usage')
  })
})
