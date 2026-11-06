import { describe, it, expect } from 'vitest'
import { groupMetricsBySource, isMetricForService, sourceFromService } from './metricGroups'

describe('metricGroups', () => {
  it('maps service names to tools', () => {
    expect(sourceFromService('claude-code')).toBe('claude_code')
    expect(sourceFromService('gemini_cli')).toBe('gemini_cli')
    expect(sourceFromService('codex_cli_rs')).toBe('codex_cli_rs')
    expect(sourceFromService('copilot-chat')).toBe('github_copilot')
    expect(sourceFromService('github-copilot')).toBe('github_copilot')
    expect(sourceFromService('opencode')).toBe('opencode')
    expect(sourceFromService('my-service')).toBeNull()
  })

  it('lists all catalog metrics without a service, and stored ones of unknown tools under Other', () => {
    const groups = groupMetricsBySource(['custom.requests'], '')
    expect(groups.claude_code).toContain('claude_code.token.usage')
    expect(groups.gemini_cli.length).toBeGreaterThan(0)
    expect(groups.other).toEqual(['custom.requests'])
    expect([...groups.claude_code].sort()).toEqual(groups.claude_code)
  })

  it('limits the catalog to the service\'s tool and files shared gen_ai metrics under it', () => {
    const groups = groupMetricsBySource(['gen_ai.client.token.usage'], 'copilot-chat')
    expect(groups.claude_code).toEqual([])
    expect(groups.github_copilot).toContain('gen_ai.client.token.usage')
    expect(groups.gemini_cli).toEqual([])
  })

  it('tells whether a metric can come from a service', () => {
    expect(isMetricForService('claude_code.token.usage', 'claude-code')).toBe(true)
    expect(isMetricForService('claude_code.token.usage', 'gemini_cli')).toBe(false)
    expect(isMetricForService('gen_ai.client.token.usage', 'opencode')).toBe(true)
    expect(isMetricForService('gen_ai.client.token.usage', 'claude-code')).toBe(false)
    expect(isMetricForService('custom.requests', 'gemini_cli')).toBe(true)
    expect(isMetricForService('claude_code.token.usage', '')).toBe(true)
    expect(isMetricForService('claude_code.token.usage', 'my-service')).toBe(true)
  })
})
