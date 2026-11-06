import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { SessionTranscriptPage } from '../SessionTranscriptPage'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import type { TranscriptResponse } from '@/types/sessions'

vi.mock('@/lib/api', () => ({
  api: { getSessionTranscript: vi.fn() },
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

const transcript: TranscriptResponse = {
  sessionId: 's1',
  serviceName: 'claude-code',
  startTime: '2026-01-02T10:00:00Z',
  lastTime: '2026-01-02T10:05:00Z',
  messages: [
    { timestamp: '2026-01-02T10:00:00Z', role: 'user', content: 'Please fix the bug', index: 0 },
    { timestamp: '2026-01-02T10:01:00Z', role: 'assistant', content: 'Looking into it', index: 1, inputTokens: 120, outputTokens: 30 },
    { timestamp: '2026-01-02T10:02:00Z', role: 'tool_use', content: '', index: 2, toolName: 'ReadFile', toolInput: '{"path":"main.go"}' },
    { timestamp: '2026-01-02T10:03:00Z', role: 'tool_result', content: 'package main', index: 3, toolName: 'ReadFile' },
    // Metadata-only response (OTLP without content), hidden by default
    { timestamp: '2026-01-02T10:04:00Z', role: 'assistant', content: '', index: 4, inputTokens: 50 },
  ],
}

function renderPage(url = '/sessions/s1') {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/sessions/:sessionId" element={<SessionTranscriptPage />} />
      </Routes>
    </MemoryRouter>
  )
}

describe('SessionTranscriptPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    vi.mocked(api.getSessionTranscript).mockResolvedValue(transcript)
  })

  it('shows a loading state, then the messages', async () => {
    renderPage()
    expect(screen.getByText('Loading transcript...')).toBeInTheDocument()

    expect(await screen.findByText('Please fix the bug')).toBeInTheDocument()
    expect(screen.getByText('Looking into it')).toBeInTheDocument()
    expect(screen.getAllByText('ReadFile')).toHaveLength(2)
    expect(screen.getByText('120 in / 30 out')).toBeInTheDocument()
  })

  it('loads the whole session, without a time range', async () => {
    renderPage('/sessions/s1?from=2026-01-02T10%3A00%3A00Z')
    await screen.findByText('Please fix the bug')
    expect(api.getSessionTranscript).toHaveBeenCalledWith('s1')
  })

  it('filters messages by role and hides metadata-only responses by default', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('Please fix the bug')

    expect(screen.getByText('Showing 4 of 5 messages')).toBeInTheDocument()
    expect(screen.queryByText('Response content not captured via OTLP telemetry')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Tool calls (1)' }))
    expect(screen.getByText('Showing 3 of 5 messages')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Hide metadata-only (1)' }))
    expect(screen.getByText('Response content not captured via OTLP telemetry')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'User (1)' }))
    expect(screen.queryByText('Please fix the bug')).not.toBeInTheDocument()
  })

  it('shows an empty transcript', async () => {
    vi.mocked(api.getSessionTranscript).mockResolvedValue({ ...transcript, messages: [] })
    renderPage()
    expect(await screen.findByText('No messages in this session')).toBeInTheDocument()
  })

  it('reports a failed request', async () => {
    vi.mocked(api.getSessionTranscript).mockRejectedValue(new Error('boom'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    renderPage()
    expect(await screen.findByText('Failed to load session transcript')).toBeInTheDocument()
    expect(toast.error).toHaveBeenCalledWith('Failed to load session transcript')
  })
})
