import { createElement, useEffect, useMemo, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api } from '@/lib/api'
import { formatTimestamp } from '@/lib/utils'
import { getServiceDisplayName, getServiceIcon } from '@/lib/metricMetadata'
import { ChatBubbleView } from '@/components/sessions/ChatBubbleView'
import { TimelineView } from '@/components/sessions/TimelineView'
import type { TranscriptResponse } from '@/types/sessions'
import { ArrowLeft, MessageSquare, Clock } from 'lucide-react'
import { toast } from 'sonner'
import { useLocalStorage } from '@/hooks/useLocalStorage'

type TranscriptMessageRole = 'user' | 'assistant' | 'tool_use' | 'tool_result'

type TranscriptFilters = {
  showUser: boolean
  showAssistant: boolean
  showToolUse: boolean
  showToolResult: boolean
  hideMetadataOnlyAssistant: boolean
}

const TRANSCRIPT_FILTERS_STORAGE_KEY = 'ai-observer-session-transcript-filters-v1'

const DEFAULT_FILTERS: TranscriptFilters = {
  showUser: true,
  showAssistant: true,
  showToolUse: true,
  showToolResult: true,
  // Sane default: hide the “Response content not captured via OTLP telemetry” placeholder entries
  hideMetadataOnlyAssistant: true,
}

function isMetadataOnlyAssistant(message: {
  role: string
  content: string
  inputTokens?: number
  outputTokens?: number
  cacheRead?: number
  cacheWrite?: number
  costUsd?: number
  durationMs?: number
}) {
  if (message.role !== 'assistant') return false
  if (message.content) return false
  return Boolean(
    message.inputTokens
    || message.outputTokens
    || message.cacheRead
    || message.cacheWrite
    || message.costUsd
    || message.durationMs
  )
}

export function SessionTranscriptPage() {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()
  const [transcript, setTranscript] = useState<TranscriptResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [viewMode, setViewMode] = useState<'chat' | 'timeline'>('chat')
  const [filters, setFilters] = useLocalStorage<TranscriptFilters>(TRANSCRIPT_FILTERS_STORAGE_KEY, DEFAULT_FILTERS)

  // Important: keep hook order stable across loading/error states
  const messages = useMemo(() => transcript?.messages ?? [], [transcript])
  const effectiveLoading = Boolean(sessionId) && loading
  const effectiveError = sessionId ? error : 'Session ID is required'

  useEffect(() => {
    if (!sessionId) {
      return
    }

    const fetchTranscript = async () => {
      setLoading(true)
      setError(null)
      try {
        const data = await api.getSessionTranscript(sessionId)
        setTranscript(data)
      } catch (err) {
        console.error('Failed to fetch transcript:', err)
        setError('Failed to load session transcript')
        toast.error('Failed to load session transcript')
      } finally {
        setLoading(false)
      }
    }

    fetchTranscript()
  }, [sessionId])

  const counts = useMemo(() => {
    const result: Record<TranscriptMessageRole, number> = {
      user: 0,
      assistant: 0,
      tool_use: 0,
      tool_result: 0,
    }
    let metadataOnlyAssistant = 0

    for (const m of messages) {
      const role = m.role as TranscriptMessageRole
      if (role in result) {
        result[role]++
      }
      if (isMetadataOnlyAssistant(m)) {
        metadataOnlyAssistant++
      }
    }

    return { ...result, metadataOnlyAssistant }
  }, [messages])

  const filteredMessages = useMemo(() => {
    return messages.filter((m) => {
      const role = m.role as TranscriptMessageRole
      if (role === 'user' && !filters.showUser) return false
      if (role === 'assistant' && !filters.showAssistant) return false
      if (role === 'tool_use' && !filters.showToolUse) return false
      if (role === 'tool_result' && !filters.showToolResult) return false

      if (filters.hideMetadataOnlyAssistant && isMetadataOnlyAssistant(m)) return false
      return true
    })
  }, [messages, filters])

  const formatDuration = (startTime: string, lastTime: string) => {
    const start = new Date(startTime)
    const end = new Date(lastTime)
    const durationMs = end.getTime() - start.getTime()

    if (durationMs < 60000) {
      return `${Math.round(durationMs / 1000)} seconds`
    } else if (durationMs < 3600000) {
      const mins = Math.round(durationMs / 60000)
      return `${mins} minute${mins !== 1 ? 's' : ''}`
    } else {
      const hours = Math.floor(durationMs / 3600000)
      const mins = Math.round((durationMs % 3600000) / 60000)
      return `${hours}h ${mins}m`
    }
  }

  if (effectiveLoading) {
    return (
      <div className="space-y-6">
        <Button
          variant="ghost"
          onClick={() => navigate('/sessions')}
          className="gap-2"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Sessions
        </Button>
        <div className="py-8 text-center text-muted-foreground">
          Loading transcript...
        </div>
      </div>
    )
  }

  if (effectiveError || !transcript) {
    return (
      <div className="space-y-6">
        <Button
          variant="ghost"
          onClick={() => navigate('/sessions')}
          className="gap-2"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Sessions
        </Button>
        <Card>
          <CardContent className="py-8">
            <div className="text-center text-muted-foreground">
              {effectiveError || 'Session not found'}
            </div>
          </CardContent>
        </Card>
      </div>
    )
  }

  const serviceIcon = getServiceIcon(transcript.serviceName)

  const setRoleFilter = (key: keyof Pick<TranscriptFilters, 'showUser' | 'showAssistant' | 'showToolUse' | 'showToolResult'>) => {
    setFilters((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  const resetFilters = () => setFilters(DEFAULT_FILTERS)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <Button
          variant="ghost"
          onClick={() => navigate('/sessions')}
          className="gap-2"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to Sessions
        </Button>
      </div>

      {/* Session Info */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-4">
            {createElement(serviceIcon, { className: 'h-10 w-10 text-muted-foreground' })}
            <div className="flex-1">
              <CardTitle className="flex items-center gap-2">
                <span className="font-mono text-base truncate max-w-[400px]">
                  {transcript.sessionId}
                </span>
                <Badge variant="outline">
                  {getServiceDisplayName(transcript.serviceName)}
                </Badge>
              </CardTitle>
              <div className="flex items-center gap-4 mt-2 text-sm text-muted-foreground">
                <span className="flex items-center gap-1">
                  <Clock className="h-4 w-4" />
                  {formatTimestamp(transcript.startTime)}
                </span>
                <span>
                  Duration: {formatDuration(transcript.startTime, transcript.lastTime)}
                </span>
                <span className="flex items-center gap-1">
                  <MessageSquare className="h-4 w-4" />
                  {transcript.messages.length} messages
                </span>
              </div>
            </div>
          </div>
        </CardHeader>
      </Card>

      {/* Transcript */}
      <Card className="overflow-hidden">
        <CardHeader>
          <div className="flex items-center justify-between gap-4 flex-wrap">
            <CardTitle>Transcript</CardTitle>
            <Tabs value={viewMode} onValueChange={(v) => setViewMode(v as 'chat' | 'timeline')}>
              <TabsList>
                <TabsTrigger value="chat">Chat</TabsTrigger>
                <TabsTrigger value="timeline">Timeline</TabsTrigger>
              </TabsList>
            </Tabs>
          </div>
          {transcript.messages.length > 0 && (
            <>
              <Separator className="my-3" />
              <div className="flex flex-col gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm text-muted-foreground mr-1">Show:</span>
                  <Button
                    type="button"
                    size="sm"
                    variant={filters.showUser ? 'secondary' : 'outline'}
                    onClick={() => setRoleFilter('showUser')}
                    disabled={counts.user === 0}
                  >
                    User ({counts.user})
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={filters.showAssistant ? 'secondary' : 'outline'}
                    onClick={() => setRoleFilter('showAssistant')}
                    disabled={counts.assistant === 0}
                  >
                    Assistant ({counts.assistant})
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={filters.showToolUse ? 'secondary' : 'outline'}
                    onClick={() => setRoleFilter('showToolUse')}
                    disabled={counts.tool_use === 0}
                  >
                    Tool calls ({counts.tool_use})
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={filters.showToolResult ? 'secondary' : 'outline'}
                    onClick={() => setRoleFilter('showToolResult')}
                    disabled={counts.tool_result === 0}
                  >
                    Tool results ({counts.tool_result})
                  </Button>

                  <Separator orientation="vertical" className="mx-1 h-6" />

                  <Button
                    type="button"
                    size="sm"
                    variant={filters.hideMetadataOnlyAssistant ? 'secondary' : 'outline'}
                    onClick={() => setFilters((prev) => ({ ...prev, hideMetadataOnlyAssistant: !prev.hideMetadataOnlyAssistant }))}
                    disabled={counts.metadataOnlyAssistant === 0}
                  >
                    Hide metadata-only ({counts.metadataOnlyAssistant})
                  </Button>

                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    onClick={resetFilters}
                    disabled={JSON.stringify(filters) === JSON.stringify(DEFAULT_FILTERS)}
                    className="ml-auto"
                  >
                    Reset
                  </Button>
                </div>

                <div className="text-xs text-muted-foreground">
                  Showing {filteredMessages.length.toLocaleString()} of {transcript.messages.length.toLocaleString()} messages
                </div>
              </div>
            </>
          )}
        </CardHeader>
        <CardContent>
          {transcript.messages.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground">
              No messages in this session
            </div>
          ) : filteredMessages.length === 0 ? (
            <div className="py-8 text-center text-muted-foreground space-y-3">
              <div>No messages match your current filters.</div>
              <div>
                <Button type="button" variant="outline" size="sm" onClick={resetFilters}>
                  Reset filters
                </Button>
              </div>
            </div>
          ) : viewMode === 'chat' ? (
            <ChatBubbleView messages={filteredMessages} />
          ) : (
            <TimelineView messages={filteredMessages} />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
