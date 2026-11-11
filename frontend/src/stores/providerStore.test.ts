import { describe, it, expect, beforeEach } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { getServiceDisplayName, serviceFilterOptions } from '@/lib/metricMetadata'
import { useServiceNames } from '@/hooks/useServiceNames'
import { useProviderStore } from './providerStore'

const providers = [
  { tool: 'claude-code', serviceName: 'claude-code', displayName: 'Claude Code', sessionFiles: true },
  { tool: 'new-tool', serviceName: 'new_tool_svc', displayName: 'New Tool', sessionFiles: false },
  {
    tool: 'multi',
    serviceName: 'multi-a',
    displayName: 'Multi A',
    sessionFiles: false,
    otherServices: [{ serviceName: 'multi-b', displayName: 'Multi B' }],
  },
]

describe('providerStore', () => {
  beforeEach(() => {
    useProviderStore.setState({ displayNames: {}, toolServices: {}, loaded: false })
  })

  it('names services with the fallback until the providers have loaded', () => {
    expect(getServiceDisplayName('codex_cli_rs')).toBe('OpenAI Codex CLI')
    expect(getServiceDisplayName('unknown-service')).toBe('unknown-service')
  })

  it('names services from the backend provider list once loaded', () => {
    useProviderStore.getState().setProviders(providers)

    expect(getServiceDisplayName('new_tool_svc')).toBe('New Tool')
    expect(getServiceDisplayName('multi-b')).toBe('Multi B')
    // Services the backend does not list keep the fallback
    expect(getServiceDisplayName('gemini_cli')).toBe('Gemini CLI')
  })

  it('takes the tools with several services from the provider list', () => {
    useProviderStore.getState().setProviders(providers)

    expect(useProviderStore.getState().toolServices).toEqual({ multi: ['multi-a', 'multi-b'] })
    expect(serviceFilterOptions(['multi-a', 'multi-b']).map((o) => o.value)).toEqual(['multi', 'multi-a', 'multi-b'])
  })

  it('re-renders components through useServiceNames when the providers arrive', () => {
    const { result } = renderHook(() => useServiceNames())
    const before = result.current
    expect(before.displayName('new_tool_svc')).toBe('new_tool_svc')
    expect(before.loaded).toBe(false)

    act(() => useProviderStore.getState().setProviders(providers))

    expect(result.current).not.toBe(before)
    expect(result.current.loaded).toBe(true)
    expect(result.current.displayName('new_tool_svc')).toBe('New Tool')
    expect(result.current.filterOptions(['claude-code'])).toEqual([{ value: 'claude-code', label: 'Claude Code' }])
  })
})
