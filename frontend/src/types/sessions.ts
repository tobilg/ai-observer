export interface Session {
  sessionId: string
  serviceName: string
  startTime: string
  lastTime: string
  messageCount: number
  model?: string
}

export interface SessionsResponse {
  sessions: Session[]
  total: number
  hasMore: boolean
  // Requests the next page (cursor parameter) when hasMore
  nextCursor?: string
}

export interface TranscriptMessage {
  timestamp: string
  role: 'user' | 'assistant' | 'tool_use' | 'tool_result'
  content: string
  index: number
  model?: string
  toolName?: string
  toolInput?: string
  toolOutput?: string      // Tool execution output (from imports)
  inputTokens?: number     // Input token count
  outputTokens?: number    // Output token count
  cacheRead?: number       // Cache read tokens
  cacheWrite?: number      // Cache write tokens
  costUsd?: number         // Cost in USD
  durationMs?: number      // Duration in milliseconds
  success?: boolean        // Tool execution success
  outputSize?: number      // Tool output size in bytes
}

export interface TranscriptResponse {
  sessionId: string
  serviceName: string
  startTime: string
  lastTime: string
  messages: TranscriptMessage[]
}
