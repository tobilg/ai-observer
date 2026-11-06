import { describe, it, expect, vi, beforeEach } from 'vitest'
import { api } from '@/lib/api'
import { getServiceDisplayName } from '@/lib/metricMetadata'
import { useProviderStore } from './providerStore'

vi.mock('@/lib/api', () => ({
  api: { getProviders: vi.fn() },
}))

describe('providerStore', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useProviderStore.setState({ displayNames: {}, loaded: false })
  })

  it('names services with the fallback until the providers have loaded', () => {
    expect(getServiceDisplayName('codex_cli_rs')).toBe('OpenAI Codex CLI')
    expect(getServiceDisplayName('unknown-service')).toBe('unknown-service')
  })

  it('names services from the backend provider list once loaded', async () => {
    vi.mocked(api.getProviders).mockResolvedValue({
      providers: [
        { tool: 'claude-code', serviceName: 'claude-code', displayName: 'Claude Code', sessionFiles: true },
        { tool: 'new-tool', serviceName: 'new_tool_svc', displayName: 'New Tool', sessionFiles: false },
      ],
    })

    await useProviderStore.getState().load()

    expect(getServiceDisplayName('new_tool_svc')).toBe('New Tool')
    // Services the backend does not list keep the fallback
    expect(getServiceDisplayName('gemini_cli')).toBe('Gemini CLI')
  })

  it('loads the providers only once', async () => {
    vi.mocked(api.getProviders).mockResolvedValue({ providers: [] })

    await useProviderStore.getState().load()
    await useProviderStore.getState().load()

    expect(api.getProviders).toHaveBeenCalledTimes(1)
  })

  it('keeps the fallback names when the request fails, and tries again later', async () => {
    vi.mocked(api.getProviders).mockRejectedValue(new Error('offline'))

    await useProviderStore.getState().load()

    expect(useProviderStore.getState().loaded).toBe(false)
    expect(getServiceDisplayName('copilot-chat')).toBe('GitHub Copilot (VS Code)')
  })
})
