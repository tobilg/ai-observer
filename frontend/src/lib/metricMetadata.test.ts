import { afterEach, describe, expect, it } from 'vitest'
import {
  getMetricMetadata,
  getMetricsBySource,
  getServiceDisplayName,
  getSourceDisplayName,
  serviceFilterOptions,
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
