import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DocSection } from '../DocSection'
import { DOCUMENTATION_SECTIONS } from '@/data/documentation'

describe('DocSection', () => {
  it('renders code blocks as written, with their label', () => {
    render(
      <DocSection
        section={{
          id: 'example',
          title: 'Example',
          content: [{ type: 'code', text: 'Codex CLI (~/.codex/config.toml)', code: '[mcp_servers.x]\nurl = "http://localhost"' }],
        }}
      />
    )
    expect(screen.getByText('Codex CLI (~/.codex/config.toml)').tagName).toBe('FIGCAPTION')
    const code = document.querySelector('pre code')
    expect(code?.textContent).toBe('[mcp_servers.x]\nurl = "http://localhost"')
  })

  it('lets table descriptions wrap', () => {
    render(
      <DocSection
        section={{
          id: 'example',
          title: 'Example',
          content: [{ type: 'table', table: { headers: ['Tool', 'Returns'], rows: [{ cells: ['get_trace', 'A trace'] }] } }],
        }}
      />
    )
    expect(screen.getByText('A trace').className).toContain('whitespace-normal')
    expect(screen.getByText('get_trace').className).toContain('font-mono')
  })
})

describe('agent access documentation', () => {
  const section = DOCUMENTATION_SECTIONS.find((s) => s.id === 'agents')

  it('lists every MCP tool', () => {
    const tools = section?.content
      .filter((c) => c.type === 'table')
      .flatMap((c) => c.table?.rows.map((r) => r.cells[0]) ?? [])
    expect(tools).toEqual(expect.arrayContaining([
      'search_conversations', 'list_sessions', 'get_transcript', 'usage_summary',
      'search_logs', 'list_traces', 'get_trace', 'list_metric_names', 'query_metrics',
    ]))
  })

  it('shows the setup of each agent', () => {
    const labels = section?.content.filter((c) => c.type === 'code').map((c) => c.text ?? '')
    for (const agent of ['Claude Code', 'OpenAI Codex CLI', 'Gemini CLI', 'GitHub Copilot in VS Code', 'OpenCode']) {
      expect(labels?.some((label) => label.startsWith(agent))).toBe(true)
    }
  })
})
