import { App } from '@modelcontextprotocol/ext-apps'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { McpApp } from './McpApp'

afterEach(() => vi.restoreAllMocks())

async function openView() {
  const connect = vi.spyOn(App.prototype, 'connect').mockResolvedValue()
  vi.spyOn(App.prototype, 'getHostCapabilities').mockReturnValue({})
  render(<McpApp />)
  await screen.findByText('Waiting for the result…')
  return connect.mock.contexts[0] as App
}

it.each([undefined, 'Stopped by the user'])('finishes a cancelled call with reason %s', async (reason) => {
  const app = await openView()
  act(() => app.ontoolcancelled!({ reason }))
  expect(screen.getByText('Tool call cancelled')).toBeVisible()
  if (reason) expect(screen.getByText(reason)).toBeVisible()
  expect(screen.queryByText('Waiting for the result…')).toBeNull()
})

it('keeps local usage controls available when the host cannot run tools', async () => {
  const app = await openView()
  act(() => app.ontoolresult!({ content: [], structuredContent: {
    from: '2026-11-01', to: '2026-11-02', group_by: 'tool', rows: [{ tool: 'codex', cost_usd: 0, total_tokens: 30 }],
    total_cost_usd: 0, unpriced_models: ['codex:sample'], note: '',
  } }))
  expect(screen.getByRole('tab', { name: 'Model' })).toBeDisabled()
  expect(screen.getByText('Token usage was recorded with no associated cost in this range.')).toBeVisible()
  fireEvent.mouseDown(screen.getByRole('tab', { name: 'Tokens' }), { button: 0 })
  expect(screen.getByRole('list', { name: 'Tokens by tool' })).toBeVisible()
})

it('labels the cost of a partial page and omits pagination on display-only hosts', async () => {
  const app = await openView()
  act(() => app.ontoolresult!({ content: [], structuredContent: {
    from: '2026-11-01', to: '2026-11-02', total: 2, next_cursor: 'next',
    sessions: [{ tool: 'codex', session_id: 'session', messages: 1, start_time: '2026-11-01', last_time: '2026-11-02', cost_usd: 1 }],
  } }))
  expect(screen.getByText(/Recorded cost of loaded sessions:.*1\.00/)).toBeVisible()
  expect(screen.getByText('Loaded sessions')).toBeVisible()
  expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull()
})

it('reveals all ranked series without a follow-up tool call', async () => {
  const app = await openView()
  act(() => app.ontoolresult!({ content: [], structuredContent: {
    name: 'custom.metric', from: '2026-11-01', to: '2026-11-02',
    series: Array.from({ length: 25 }, (_, i) => ({ labels: { model: String(i) }, points: [{ value: i }] })),
  } }))
  expect(screen.getAllByRole('listitem')).toHaveLength(20)
  fireEvent.click(screen.getByRole('button', { name: 'Show 5 more' }))
  expect(screen.getAllByRole('listitem')).toHaveLength(25)
})
