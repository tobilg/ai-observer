import { cn } from '@/lib/utils'
import { formatTimestamp } from '@/lib/utils'
import { ToolCallBlock } from './ToolCallBlock'
import { Markdown } from '@/components/ui/markdown'
import type { TranscriptMessage } from '@/types/sessions'
import { User, Bot, Coins, Clock, Zap } from 'lucide-react'

interface ChatBubbleViewProps {
  messages: TranscriptMessage[]
  className?: string
}

export function ChatBubbleView({ messages, className }: ChatBubbleViewProps) {
  const formatTokens = (input?: number, output?: number, cacheRead?: number, cacheWrite?: number) => {
    const parts: string[] = []
    if (input) parts.push(`${input.toLocaleString()} in`)
    if (output) parts.push(`${output.toLocaleString()} out`)
    if (cacheRead) parts.push(`${cacheRead.toLocaleString()} cache`)
    if (cacheWrite) parts.push(`${cacheWrite.toLocaleString()} cached`)
    return parts.join(' / ')
  }

  const formatCost = (cost?: number) => {
    if (!cost) return null
    return `$${cost.toFixed(4)}`
  }

  const formatDuration = (ms?: number) => {
    if (!ms) return null
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    return `${(ms / 60000).toFixed(1)}m`
  }

  return (
    <div className={cn('space-y-4 w-full min-w-0 overflow-hidden', className)}>
      {messages.map((message, index) => {
        const isUser = message.role === 'user'
        const isToolUse = message.role === 'tool_use'
        const isToolResult = message.role === 'tool_result'
        const isAssistant = message.role === 'assistant'

        // Tool calls are shown inline as collapsible blocks
        if (isToolUse || isToolResult) {
          return (
            <div key={index} className="flex justify-start">
              <div className="max-w-[85%] min-w-0">
                <ToolCallBlock
                  toolName={message.toolName || 'Tool'}
                  toolInput={message.toolInput}
                  toolOutput={message.toolOutput}
                  content={isToolResult ? message.content : undefined}
                  durationMs={message.durationMs}
                  success={message.success}
                  outputSize={message.outputSize}
                />
                <div className="text-xs text-muted-foreground mt-1 ml-1">
                  {formatTimestamp(message.timestamp)}
                </div>
              </div>
            </div>
          )
        }

        // Check if assistant message has metadata but no content (OTLP case)
        const hasMetadataOnly = isAssistant && !message.content && (message.inputTokens || message.costUsd)
        const tokenInfo = formatTokens(message.inputTokens, message.outputTokens, message.cacheRead, message.cacheWrite)
        const costInfo = formatCost(message.costUsd)
        const durationInfo = formatDuration(message.durationMs)

        return (
          <div
            key={index}
            className={cn(
              'flex gap-3',
              isUser ? 'justify-end' : 'justify-start'
            )}
          >
            {!isUser && (
              <div className="shrink-0 w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center">
                <Bot className="h-4 w-4 text-primary" />
              </div>
            )}

            <div
              className={cn(
                'max-w-[80%] min-w-0 rounded-2xl px-4 py-3 overflow-hidden',
                isUser
                  ? 'bg-primary text-primary-foreground rounded-br-md'
                  : 'bg-muted rounded-bl-md'
              )}
            >
              {hasMetadataOnly ? (
                // Show metadata-only view for OTLP assistant events
                <div className="text-sm text-muted-foreground italic">
                  Response content not captured via OTLP telemetry
                </div>
              ) : isAssistant ? (
                // Render assistant messages as markdown
                <Markdown className="text-sm">
                  {message.content}
                </Markdown>
              ) : (
                // User messages as plain text
                <div className="whitespace-pre-wrap text-sm break-all [overflow-wrap:anywhere]">
                  {message.content}
                </div>
              )}

              {/* Metadata line for assistant messages */}
              {isAssistant && (tokenInfo || costInfo || durationInfo) && (
                <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground mt-2 pt-2 border-t border-border/50">
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

              <div
                className={cn(
                  'text-xs mt-2',
                  isUser ? 'text-primary-foreground/70' : 'text-muted-foreground'
                )}
              >
                {formatTimestamp(message.timestamp)}
                {message.model && !isUser && (
                  <span className="ml-2">{message.model}</span>
                )}
              </div>
            </div>

            {isUser && (
              <div className="shrink-0 w-8 h-8 rounded-full bg-primary flex items-center justify-center">
                <User className="h-4 w-4 text-primary-foreground" />
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
