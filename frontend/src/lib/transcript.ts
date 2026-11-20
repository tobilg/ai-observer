// Session transcripts: the pages of the transcript query, role filters and
// counts (SessionTranscriptPage)
import type { InfiniteData } from '@tanstack/react-query'
import type { TranscriptMessage, TranscriptResponse } from '@/types/sessions'

export type TranscriptRole = TranscriptMessage['role']

// The data of useSessionTranscript: pages in order, cursors of each
export type TranscriptData = InfiniteData<TranscriptResponse, unknown>

export function transcriptMessages(data: TranscriptData | undefined): TranscriptMessage[] {
  return data?.pages.flatMap((page) => page.messages ?? []) ?? []
}

export interface TranscriptFilters {
  showUser: boolean
  showAssistant: boolean
  showToolUse: boolean
  showToolResult: boolean
  // Responses of OTLP events without content (tokens and cost only)
  hideMetadataOnlyAssistant: boolean
}

export const DEFAULT_TRANSCRIPT_FILTERS: TranscriptFilters = {
  showUser: true,
  showAssistant: true,
  showToolUse: true,
  showToolResult: true,
  // Hide the “Response content not captured via OTLP telemetry” placeholders
  hideMetadataOnlyAssistant: true,
}

// System messages (API errors, OpenCode session events) have no filter and
// are always shown
const ROLE_FILTERS: Record<Exclude<TranscriptRole, 'system'>, keyof TranscriptFilters> = {
  user: 'showUser',
  assistant: 'showAssistant',
  tool_use: 'showToolUse',
  tool_result: 'showToolResult',
}

export function isMetadataOnlyAssistant(message: TranscriptMessage): boolean {
  if (message.role !== 'assistant' || message.content) {
    return false
  }
  return Boolean(
    message.inputTokens || message.outputTokens || message.cacheRead || message.cacheWrite || message.costUsd || message.durationMs,
  )
}

export function passesFilters(message: TranscriptMessage, filters: TranscriptFilters): boolean {
  const key = message.role === 'system' ? undefined : ROLE_FILTERS[message.role]
  if (key && !filters[key]) {
    return false
  }
  return !(filters.hideMetadataOnlyAssistant && isMetadataOnlyAssistant(message))
}

/**
 * The messages the filters let through, with their positions in the whole
 * transcript; the message at alwaysShow (the current search match) is kept
 * even when the filters would hide it
 */
export function filterTranscript(
  messages: TranscriptMessage[],
  filters: TranscriptFilters,
  alwaysShow?: number,
): { messages: TranscriptMessage[]; positions: number[] } {
  const shown: TranscriptMessage[] = []
  const positions: number[] = []
  messages.forEach((message, position) => {
    if (position === alwaysShow || passesFilters(message, filters)) {
      shown.push(message)
      positions.push(position)
    }
  })
  return { messages: shown, positions }
}

export interface RoleCounts {
  user: number
  assistant: number
  tool_use: number
  tool_result: number
  metadataOnlyAssistant: number
}

export function countRoles(messages: TranscriptMessage[]): RoleCounts {
  const counts: RoleCounts = { user: 0, assistant: 0, tool_use: 0, tool_result: 0, metadataOnlyAssistant: 0 }
  for (const message of messages) {
    if (message.role !== 'system') {
      counts[message.role]++
    }
    if (isMetadataOnlyAssistant(message)) {
      counts.metadataOnlyAssistant++
    }
  }
  return counts
}

/** Tool input as sent, JSON pretty-printed */
export function prettyJSON(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}
