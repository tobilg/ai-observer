import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ChatBubbleView } from '../ChatBubbleView'
import type { TranscriptMessage } from '@/types/sessions'

const at = '2026-01-02T10:00:00Z'

describe('ChatBubbleView', () => {
  beforeEach(() => {
    // The virtualizer scrolls the window to where it is when it mounts
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  })

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
    expect(screen.queryByRole('button', { name: 'Copy message' })).not.toBeInTheDocument()
  })

  it('copies the content of a message', async () => {
    const user = userEvent.setup()
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    render(<ChatBubbleView messages={[{ timestamp: at, role: 'user', content: 'Fix the bug', index: 0 }]} />)

    await user.click(screen.getByRole('button', { name: 'Copy message' }))
    expect(writeText).toHaveBeenCalledWith('Fix the bug')
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()
  })

  it('shows tool calls as blocks that expand to their input and output, with copy buttons', async () => {
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
    const block = screen.getByRole('button', { name: /ReadFile/ })
    expect(block).toHaveAttribute('aria-expanded', 'false')
    await user.click(block)
    expect(block).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('Input')).toBeInTheDocument()
    expect(screen.getByText('file contents')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy tool input' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy tool output' })).toBeInTheDocument()
  })

  it('keeps expanded tool blocks by transcript position while the list changes', async () => {
    const user = userEvent.setup()
    const messages: TranscriptMessage[] = [
      { timestamp: at, role: 'user', content: 'Read it', index: 0 },
      { timestamp: at, role: 'tool_result', content: 'file contents', index: 1, toolName: 'ReadFile' },
    ]
    const { rerender } = render(<ChatBubbleView messages={messages} />)
    await user.click(screen.getByRole('button', { name: /ReadFile/ }))
    expect(screen.getByText('file contents')).toBeInTheDocument()

    // A filter hides the first message: the block is still expanded
    rerender(<ChatBubbleView messages={messages.slice(1)} positions={[1]} />)
    expect(screen.getByText('file contents')).toBeInTheDocument()
    // The message leaves the list (as when it scrolls away) and comes back
    rerender(<ChatBubbleView messages={messages.slice(0, 1)} positions={[0]} />)
    expect(screen.queryByText('file contents')).not.toBeInTheDocument()
    rerender(<ChatBubbleView messages={messages} />)
    expect(screen.getByText('file contents')).toBeInTheDocument()
  })

  it('marks search terms and the current match, with its tool block expanded', () => {
    render(
      <ChatBubbleView
        messages={[
          { timestamp: at, role: 'user', content: 'Where is the login bug?', index: 0 },
          { timestamp: at, role: 'assistant', content: 'The **login** form', index: 1 },
          { timestamp: at, role: 'tool_result', content: 'login.go', index: 2, toolName: 'Grep' },
        ]}
        highlight={['login']}
        activePosition={2}
        revealKey="login#0"
      />
    )

    const marks = screen.getAllByText(/^login$/i, { selector: 'mark' })
    expect(marks.length).toBe(3)
    expect(document.querySelector('[data-active]')).toHaveAttribute('data-position', '2')
    expect(screen.getByRole('button', { name: /Grep/ })).toHaveAttribute('aria-expanded', 'true')
  })
})
