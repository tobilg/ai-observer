import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ChatBubbleView } from '../ChatBubbleView'
import type { TranscriptMessage } from '@/types/sessions'

const at = '2026-01-02T10:00:00Z'

describe('ChatBubbleView', () => {
  it('renders user text as typed and assistant text as markdown', () => {
    const messages: TranscriptMessage[] = [
      { timestamp: at, role: 'user', content: 'use **bold**?', index: 0 },
      { timestamp: at, role: 'assistant', content: 'Use **bold** like this', index: 1 },
    ]
    render(<ChatBubbleView messages={messages} />)

    expect(screen.getByText('use **bold**?')).toBeInTheDocument()
    expect(screen.getByText('bold').tagName).toBe('STRONG')
  })

  it('shows token usage, cost and duration of assistant responses', () => {
    render(
      <ChatBubbleView
        messages={[{
          timestamp: at, role: 'assistant', content: 'Done', index: 0,
          inputTokens: 120, outputTokens: 30, cacheRead: 50, costUsd: 0.01234, durationMs: 2500,
        }]}
      />
    )

    expect(screen.getByText('120 in / 30 out / 50 cache')).toBeInTheDocument()
    expect(screen.getByText('$0.0123')).toBeInTheDocument()
    expect(screen.getByText('2.5s')).toBeInTheDocument()
  })

  it('marks responses whose content was not captured', () => {
    render(<ChatBubbleView messages={[{ timestamp: at, role: 'assistant', content: '', index: 0, inputTokens: 10 }]} />)
    expect(screen.getByText('Response content not captured via OTLP telemetry')).toBeInTheDocument()
  })

  it('shows tool calls as blocks that expand to their input and output', async () => {
    const user = userEvent.setup()
    render(
      <ChatBubbleView
        messages={[{
          timestamp: at, role: 'tool_result', content: 'file contents', index: 0,
          toolName: 'ReadFile', toolInput: '{"path":"main.go"}', success: true,
        }]}
      />
    )

    expect(screen.queryByText('file contents')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /ReadFile/ }))
    expect(screen.getByText('Input')).toBeInTheDocument()
    expect(screen.getByText('file contents')).toBeInTheDocument()
  })
})
