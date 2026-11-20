import { describe, it, expect } from 'vitest'
import {
  countRoles,
  DEFAULT_TRANSCRIPT_FILTERS,
  filterTranscript,
  isMetadataOnlyAssistant,
  prettyJSON,
  transcriptMessages,
  type TranscriptData,
} from './transcript'
import type { TranscriptMessage, TranscriptResponse } from '@/types/sessions'

const at = '2026-01-02T10:00:00Z'
const msg = (index: number, role: TranscriptMessage['role'], fields: Partial<TranscriptMessage> = {}): TranscriptMessage => ({
  timestamp: at, role, content: `m${index}`, index, ...fields,
})
const page = (messages: TranscriptMessage[], fields: Partial<TranscriptResponse> = {}): TranscriptResponse => ({
  sessionId: 's', serviceName: 'claude-code', startTime: at, lastTime: at, messages, total: messages.length, hasMore: false, ...fields,
})

describe('transcript', () => {
  const data: TranscriptData = {
    pages: [page([msg(0, 'user'), msg(1, 'assistant')]), page([msg(2, 'tool_use')])],
    pageParams: [undefined, '2'],
  }

  it('reads the loaded messages of all pages in order', () => {
    expect(transcriptMessages(data).map((m) => m.index)).toEqual([0, 1, 2])
    expect(transcriptMessages(undefined)).toEqual([])
  })

  it('filters by role and keeps the positions in the transcript', () => {
    const messages = [msg(0, 'user'), msg(1, 'tool_use'), msg(2, 'assistant', { content: '', inputTokens: 5 }), msg(3, 'assistant')]
    const filters = { ...DEFAULT_TRANSCRIPT_FILTERS, showToolUse: false }
    expect(filterTranscript(messages, filters).positions).toEqual([0, 3])
    // The current search match is kept
    expect(filterTranscript(messages, filters, 1).positions).toEqual([0, 1, 3])
    expect(filterTranscript(messages, { ...filters, hideMetadataOnlyAssistant: false }).positions).toEqual([0, 2, 3])
  })

  it('counts roles and responses without content', () => {
    const messages = [msg(0, 'user'), msg(1, 'assistant', { content: '', costUsd: 0.1 }), msg(2, 'tool_result')]
    expect(isMetadataOnlyAssistant(messages[1])).toBe(true)
    expect(isMetadataOnlyAssistant(msg(3, 'assistant', { content: '' }))).toBe(false)
    expect(countRoles(messages)).toEqual({ user: 1, assistant: 1, tool_use: 0, tool_result: 1, metadataOnlyAssistant: 1 })
  })

  it('always shows system messages and counts them under no role', () => {
    const messages = [msg(0, 'user'), msg(1, 'system', { content: 'API error' })]
    const allHidden = { ...DEFAULT_TRANSCRIPT_FILTERS, showUser: false, showAssistant: false, showToolUse: false, showToolResult: false }
    expect(filterTranscript(messages, allHidden).positions).toEqual([1])
    expect(countRoles(messages)).toEqual({ user: 1, assistant: 0, tool_use: 0, tool_result: 0, metadataOnlyAssistant: 0 })
  })

  it('pretty-prints JSON tool input', () => {
    expect(prettyJSON('{"a":1}')).toBe('{\n  "a": 1\n}')
    expect(prettyJSON('ls -la')).toBe('ls -la')
  })
})
