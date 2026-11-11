import { afterEach, describe, expect, it } from 'vitest'
import {
  UNIT_FORMATS,
  breakdownOptions,
  getMetricMetadata,
  getMetricsBySource,
  getServiceDisplayName,
  getSourceDisplayName,
  isAdditiveStat,
  isHistogramMetric,
  serviceFilterOptions,
  seriesStat,
  statParam,
  statUnit,
} from './metricMetadata'
import { useProviderStore } from '@/stores/providerStore'

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

describe('Codex services', () => {
  afterEach(() => {
    useProviderStore.setState({ displayNames: {}, toolServices: {}, loaded: false })
  })

  it('offers all of Codex before its services when it sends several', () => {
    const services = ['claude-code', 'codex-app-server', 'codex-exec-server', 'codex_cli_rs']
    expect(serviceFilterOptions(services)).toEqual([
      { value: 'claude-code', label: 'Claude Code' },
      { value: 'codex', label: 'OpenAI Codex (all services)' },
      { value: 'codex-app-server', label: 'OpenAI Codex' },
      { value: 'codex-exec-server', label: 'OpenAI Codex exec server' },
      { value: 'codex_cli_rs', label: 'OpenAI Codex CLI' },
    ])
    expect(getServiceDisplayName('codex')).toBe('OpenAI Codex (all services)')
  })

  it('offers no tool entry for a single Codex service', () => {
    expect(serviceFilterOptions(['codex_cli_rs', 'gemini_cli']).map((o) => o.value)).toEqual(['codex_cli_rs', 'gemini_cli'])
  })

  it('takes the tools with several services from the provider list', () => {
    useProviderStore.setState({
      displayNames: { codex_cli_rs: 'OpenAI Codex CLI', 'codex-app-server': 'OpenAI Codex' },
      toolServices: { codex: ['codex_cli_rs', 'codex-app-server'] },
      loaded: true,
    })
    expect(serviceFilterOptions(['codex-app-server', 'codex_cli_rs']).map((o) => o.value)).toEqual([
      'codex',
      'codex-app-server',
      'codex_cli_rs',
    ])
  })

  it("groups the exec server's metrics with Codex", () => {
    expect(getMetricMetadata('exec_server_processes_active').source).toBe('codex_cli_rs')
    expect(getMetricMetadata('codex.turn.token_usage').metricType).toBe('histogram')
  })
})

describe('histogram statistics and breakdowns', () => {
  const latency = getMetricMetadata('gemini_cli.api.request.latency')
  const tokens = getMetricMetadata('gen_ai.client.token.usage')
  const counter = getMetricMetadata('claude_code.token.usage')

  it('defaults durations to the average and token counts to the sum', () => {
    expect(isHistogramMetric(latency)).toBe(true)
    expect(isHistogramMetric(counter)).toBe(false)
    expect(seriesStat(latency)).toBe('avg')
    expect(seriesStat(tokens)).toBe('sum')
    expect(seriesStat(latency, 'p99')).toBe('p99')
    expect(seriesStat(latency, 'p42')).toBe('avg')
    expect(seriesStat(counter, 'p99')).toBeUndefined()
    // The server's default is not sent
    expect(statParam(latency)).toBeUndefined()
    expect(statParam(tokens)).toBe('sum')
    expect(statParam(tokens, 'avg')).toBeUndefined()
  })

  it('knows which statistics add up and that counts are numbers', () => {
    expect(isAdditiveStat(undefined)).toBe(true)
    expect(isAdditiveStat('sum')).toBe(true)
    expect(isAdditiveStat('count')).toBe(true)
    expect(isAdditiveStat('avg')).toBe(false)
    expect(isAdditiveStat('p95')).toBe(false)
    expect(statUnit(latency.unit, 'p95')).toBe(latency.unit)
    expect(statUnit(latency.unit, 'count')).toBe(UNIT_FORMATS.count)
  })

  it('offers the catalog breakdowns by name, then the other attribute keys', () => {
    const options = breakdownOptions(counter, ['session.id', 'type', 'model'])
    const known = (counter.breakdowns ?? []).map((b) => b.attributeKey)
    expect(options.slice(0, known.length).map((o) => o.value)).toEqual(known)
    expect(options.filter((o) => !o.known).map((o) => o.value)).toEqual(['session.id'].filter((k) => !known.includes(k)))
    expect(new Set(options.map((o) => o.value)).size).toBe(options.length)
    expect(breakdownOptions(null, ['b', 'a']).map((o) => o.label)).toEqual(['a', 'b'])
  })
})
