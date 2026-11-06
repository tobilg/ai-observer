import { useState } from 'react'
import { cn, formatTimestamp } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/common/markdown'
import { CodeBlock } from '@/components/sessions/CodeBlock'
import { ChevronDown, ChevronUp, User, Bot, Wrench, CheckCircle, XCircle, Zap, Coins, Clock, FileText } from 'lucide-react'
import type { TranscriptMessage } from '@/types/sessions'

interface TimelineViewProps {
  messages: TranscriptMessage[]
  className?: string
}

function getRoleConfig(role: string) {
  switch (role) {
    case 'user':
      return {
        icon: User,
        label: 'User',
        color: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
        lineColor: 'bg-blue-500',
      }
    case 'assistant':
      return {
        icon: Bot,
        label: 'Assistant',
        color: 'bg-green-500/10 text-green-500 border-green-500/20',
        lineColor: 'bg-green-500',
      }
    case 'tool_use':
      return {
        icon: Wrench,
        label: 'Tool Call',
        color: 'bg-orange-500/10 text-orange-500 border-orange-500/20',
        lineColor: 'bg-orange-500',
      }
    case 'tool_result':
      return {
        icon: CheckCircle,
        label: 'Tool Result',
        color: 'bg-purple-500/10 text-purple-500 border-purple-500/20',
        lineColor: 'bg-purple-500',
      }
    default:
      return {
        icon: Bot,
        label: role,
        color: 'bg-muted text-muted-foreground border-muted',
        lineColor: 'bg-muted-foreground',
      }
  }
}

// Helper functions for formatting
function formatTokens(input?: number, output?: number, cacheRead?: number, cacheWrite?: number) {
  const parts: string[] = []
  if (input) parts.push(`${input.toLocaleString()} in`)
  if (output) parts.push(`${output.toLocaleString()} out`)
  if (cacheRead) parts.push(`${cacheRead.toLocaleString()} cache`)
  if (cacheWrite) parts.push(`${cacheWrite.toLocaleString()} cached`)
  return parts.length > 0 ? parts.join(' / ') : null
}

function formatCost(cost?: number) {
  if (!cost) return null
  return `$${cost.toFixed(4)}`
}

function formatDuration(ms?: number) {
  if (!ms) return null
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60000).toFixed(1)}m`
}

function formatBytes(bytes?: number) {
  if (!bytes) return null
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function TimelineItem({ message, isLast }: { message: TranscriptMessage; isLast: boolean }) {
  const [isExpanded, setIsExpanded] = useState(false)
  const config = getRoleConfig(message.role)
  const Icon = config.icon

  const hasExpandableContent = message.content && message.content.length > 200
  const displayContent = isExpanded || !hasExpandableContent
    ? message.content
    : message.content.slice(0, 200) + '...'

  const hasToolDetails = message.toolName || message.toolInput
  const isToolRole = message.role === 'tool_use' || message.role === 'tool_result'
  const isAssistant = message.role === 'assistant'

  // Metadata values
  const tokenInfo = formatTokens(message.inputTokens, message.outputTokens, message.cacheRead, message.cacheWrite)
  const costInfo = formatCost(message.costUsd)
  const durationInfo = formatDuration(message.durationMs)
  const hasMetadata = tokenInfo || costInfo || durationInfo

  // Check if this is metadata-only (OTLP case for assistant)
  const hasMetadataOnly = isAssistant && !message.content && hasMetadata

  return (
    <div className="relative flex gap-4">
      {/* Timeline line */}
      {!isLast && (
        <div
          className={cn(
            'absolute left-[15px] top-10 bottom-0 w-0.5',
            config.lineColor,
            'opacity-30'
          )}
        />
      )}

      {/* Timeline dot */}
      <div
        className={cn(
          'shrink-0 w-8 h-8 rounded-full flex items-center justify-center',
          config.color,
          'border'
        )}
      >
        <Icon className="h-4 w-4" />
      </div>

      {/* Content */}
      <div className="flex-1 pb-6 min-w-0">
        <div className="flex items-center gap-2 mb-1 flex-wrap">
          <Badge variant="outline" className={cn('text-xs', config.color)}>
            {config.label}
          </Badge>
          <span className="text-xs text-muted-foreground">
            {formatTimestamp(message.timestamp)}
          </span>
          {message.model && (
            <span className="text-xs text-muted-foreground">
              {message.model}
            </span>
          )}
          {/* Tool success/failure indicator */}
          {isToolRole && message.success !== undefined && (
            message.success ? (
              <CheckCircle className="h-3.5 w-3.5 text-green-500" />
            ) : (
              <XCircle className="h-3.5 w-3.5 text-red-500" />
            )
          )}
          {/* Tool duration */}
          {isToolRole && message.durationMs !== undefined && message.durationMs > 0 && (
            <Badge variant="secondary" className="text-xs py-0 h-5">
              <Clock className="h-3 w-3 mr-1" />
              {formatDuration(message.durationMs)}
            </Badge>
          )}
        </div>

        {/* Metadata row for assistant messages */}
        {isAssistant && hasMetadata && (
          <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground mb-2">
            {tokenInfo && (
              <span className="flex items-center gap-1">
                <Zap className="h-3 w-3" />
                {tokenInfo}
              </span>
            )}
            {costInfo && (
              <span className="flex items-center gap-1">
                <Coins className="h-3 w-3" />
                {costInfo}
              </span>
            )}
            {durationInfo && (
              <span className="flex items-center gap-1">
                <Clock className="h-3 w-3" />
                {durationInfo}
              </span>
            )}
          </div>
        )}

        {/* Tool details */}
        {hasToolDetails && (
          <div className="mb-2">
            {message.toolName && (
              <span className="font-mono text-sm font-medium">
                {message.toolName}
              </span>
            )}
            {message.toolInput && (
              <details className="mt-1">
                <summary className="text-xs text-muted-foreground cursor-pointer hover:text-foreground">
                  View input
                </summary>
                <div className="mt-1">
                  <CodeBlock
                    text={(() => {
                      try {
                        return JSON.stringify(JSON.parse(message.toolInput), null, 2)
                      } catch {
                        return message.toolInput
                      }
                    })()}
                    maxHeightClassName="max-h-48 overflow-y-auto"
                  />
                </div>
              </details>
            )}
            {/* Tool output (from imports) */}
            {message.toolOutput && (
              <details className="mt-1">
                <summary className="text-xs text-muted-foreground cursor-pointer hover:text-foreground">
                  View output
                </summary>
                <div className="mt-1">
                  <CodeBlock
                    text={message.toolOutput}
                    maxHeightClassName="max-h-96 overflow-y-auto"
                  />
                </div>
              </details>
            )}
            {/* Output size indicator when no actual output */}
            {!message.toolOutput && message.outputSize !== undefined && message.outputSize > 0 && (
              <div className="mt-1 text-xs text-muted-foreground flex items-center gap-1">
                <FileText className="h-3 w-3" />
                Output not captured via OTLP ({formatBytes(message.outputSize)})
              </div>
            )}
          </div>
        )}

        {/* Message content */}
        {hasMetadataOnly ? (
          <div className="bg-muted/50 rounded-lg p-3">
            <span className="text-sm text-muted-foreground italic">
              Response content not captured via OTLP telemetry
            </span>
          </div>
        ) : message.content ? (
          <div className="bg-muted/50 rounded-lg p-3 w-full max-w-full min-w-0 overflow-x-auto">
            {isAssistant ? (
              <Markdown className="text-sm">
                {displayContent || ''}
              </Markdown>
            ) : (
              <pre className="text-sm whitespace-pre-wrap font-sans max-w-full min-w-0 break-all [overflow-wrap:anywhere]">
                {displayContent}
              </pre>
            )}
            {hasExpandableContent && (
              <Button
                variant="ghost"
                size="sm"
                className="mt-2 h-6 text-xs"
                onClick={() => setIsExpanded(!isExpanded)}
              >
                {isExpanded ? (
                  <>
                    <ChevronUp className="h-3 w-3 mr-1" />
                    Show less
                  </>
                ) : (
                  <>
                    <ChevronDown className="h-3 w-3 mr-1" />
                    Show more
                  </>
                )}
              </Button>
            )}
          </div>
        ) : null}
      </div>
    </div>
  )
}

export function TimelineView({ messages, className }: TimelineViewProps) {
  return (
    <div className={cn('space-y-0', className)}>
      {messages.map((message, index) => (
        <TimelineItem
          key={index}
          message={message}
          isLast={index === messages.length - 1}
        />
      ))}
    </div>
  )
}
