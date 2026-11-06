import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TimelineView } from '../TimelineView'
import type { TranscriptMessage } from '@/types/sessions'

const at = '2026-01-02T10:00:00Z'

function message(fields: Partial<TranscriptMessage> & Pick<TranscriptMessage, 'role'>): TranscriptMessage {
  return { timestamp: at, content: '', index: 0, ...fields }
}

describe('TimelineView', () => {
  it('labels each message by its role', () => {
    render(
      <TimelineView
        messages={[
          message({ role: 'user', content: 'Fix the bug' }),
          message({ role: 'assistant', content: 'On it' }),
          message({ role: 'tool_use', toolName: 'ReadFile' }),
          message({ role: 'tool_result', toolName: 'ReadFile', content: 'file contents' }),
        ]}
      />
    )

    expect(screen.getByText('User')).toBeInTheDocument()
    expect(screen.getByText('Assistant')).toBeInTheDocument()
    expect(screen.getByText('Tool Call')).toBeInTheDocument()
    expect(screen.getByText('Tool Result')).toBeInTheDocument()
    expect(screen.getByText('Fix the bug')).toBeInTheDocument()
    expect(screen.getAllByText('ReadFile')).toHaveLength(2)
  })

  it('shows the token usage, cost and duration of assistant responses', () => {
    render(
      <TimelineView
        messages={[message({
          role: 'assistant', content: 'Done', model: 'gemini-2.5-pro',
          inputTokens: 120, outputTokens: 30, cacheRead: 50, cacheWrite: 10, costUsd: 0.01234, durationMs: 1500,
        })]}
      />
    )

    expect(screen.getByText('120 in / 30 out / 50 cache / 10 cached')).toBeInTheDocument()
    expect(screen.getByText('$0.0123')).toBeInTheDocument()
    expect(screen.getByText('1.5s')).toBeInTheDocument()
    expect(screen.getByText('gemini-2.5-pro')).toBeInTheDocument()
  })

  it('says when an assistant response has metadata but no content', () => {
    render(<TimelineView messages={[message({ role: 'assistant', outputTokens: 30 })]} />)
    expect(screen.getByText('Response content not captured via OTLP telemetry')).toBeInTheDocument()
  })

  it('pretty-prints JSON tool input and keeps other input as it is', () => {
    const { container } = render(
      <TimelineView
        messages={[
          message({ role: 'tool_use', toolName: 'Edit', toolInput: '{"path":"a.go","line":3}' }),
          message({ role: 'tool_use', toolName: 'Bash', toolInput: 'ls -la' }),
        ]}
      />
    )

    expect(screen.getAllByText('View input')).toHaveLength(2)
    const inputs = Array.from(container.querySelectorAll('details')).map((d) => d.textContent)
    expect(inputs[0]).toContain('{\n  "path": "a.go",\n  "line": 3\n}')
    expect(inputs[1]).toContain('ls -la')
  })

  it('shows imported tool output, or the size of output OTLP did not capture', () => {
    render(
      <TimelineView
        messages={[
          message({ role: 'tool_result', toolName: 'Read', toolOutput: 'line 1\nline 2', durationMs: 250 }),
          message({ role: 'tool_result', toolName: 'Grep', outputSize: 1536 }),
        ]}
      />
    )

    expect(screen.getByText('View output')).toBeInTheDocument()
    expect(screen.getByText('250ms')).toBeInTheDocument()
    expect(screen.getByText('Output not captured via OTLP (1.5 KB)')).toBeInTheDocument()
  })

  it('shortens long content until it is expanded', async () => {
    const user = userEvent.setup()
    const long = 'x'.repeat(250)
    render(<TimelineView messages={[message({ role: 'user', content: long })]} />)

    expect(screen.getByText('x'.repeat(200) + '...')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Show more' }))
    expect(screen.getByText(long)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Show less' }))
    expect(screen.getByText('x'.repeat(200) + '...')).toBeInTheDocument()
  })
})
