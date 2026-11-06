export interface LogRecord {
  timestamp: string
  traceId?: string
  spanId?: string
  traceFlags?: number
  severityText?: string
  severityNumber?: number
  serviceName: string
  body?: string
  resourceSchemaUrl?: string
  resourceAttributes?: Record<string, string>
  scopeSchemaUrl?: string
  scopeName?: string
  scopeVersion?: string
  scopeAttributes?: Record<string, string>
  logAttributes?: Record<string, string>
}

export interface LogsResponse {
  logs: LogRecord[]
  total: number
  hasMore: boolean
  // Requests the next page (cursor parameter) when hasMore
  nextCursor?: string
}

export interface LogLevelsResponse {
  [level: string]: number
}
