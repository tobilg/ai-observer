import { memo, useCallback } from 'react'
import { User, Bot, Coins, Clock, Zap } from 'lucide-react'
import { cn, formatTimestamp } from '@/lib/utils'
import { formatCost, formatDurationMs, formatTokenUsage } from '@/lib/format'
import { useExpansion } from '@/hooks/useExpansion'
import { CopyButton } from '@/components/common/copy-button'
import { HighlightedText } from '@/components/common/highlighted-text'
import { Markdown } from '@/components/common/markdown'
import { ToolCallBlock } from './ToolCallBlock'
import { TranscriptList } from './TranscriptList'
import type { TranscriptViewProps } from './types'
import type { TranscriptMessage } from '@/types/sessions'

// Height of a message before it is measured
const ESTIMATED_HEIGHT = 120

export function ChatBubbleView({ messages, positions, highlight, activePosition, revealKey, className }: TranscriptViewProps) {
  const expansion = useExpansion()
  const { isExpanded, toggle } = expansion
  const positionAt = useCallback((index: number) => positions?.[index] ?? index, [positions])
  const activeIndex = activePosition === undefined ? undefined : (positions ? positions.indexOf(activePosition) : activePosition)

  const renderItem = (index: number) => {
    const position = positionAt(index)
    const active = position === activePosition
    const toolExpanded = isExpanded(position, 'tool', active)
    return (
      <ChatMessage
        message={messages[index]}
        position={position}
        active={active}
        highlight={highlight}
        toolExpanded={toolExpanded}
        onToggle={toggle}
      />
    )
  }

  return (
    <TranscriptList
      className={cn('overflow-hidden', className)}
      count={messages.length}
      itemKey={positionAt}
      renderItem={renderItem}
      estimateSize={ESTIMATED_HEIGHT}
      revealIndex={activeIndex}
      revealKey={revealKey}
    />
  )
}

interface ChatMessageProps {
  message: TranscriptMessage
  position: number
  // The current search match
  active: boolean
  highlight?: readonly string[]
  toolExpanded: boolean
  onToggle: (position: number, part: string, current: boolean) => void
}

// One message; memoized, since the list renders again on every scroll
const ChatMessage = memo(function ChatMessage({ message, position, active, highlight, toolExpanded, onToggle }: ChatMessageProps) {
  const isUser = message.role === 'user'
  const isToolUse = message.role === 'tool_use'
  const isToolResult = message.role === 'tool_result'
  const isAssistant = message.role === 'assistant'

  // Tool calls are shown inline as collapsible blocks
  if (isToolUse || isToolResult) {
    return (
      <div className="flex justify-start" data-position={position} data-active={active || undefined}>
        <div className={cn('max-w-[85%] min-w-0 rounded-lg', active && 'ring-2 ring-warning')}>
          <ToolCallBlock
            toolName={message.toolName || 'Tool'}
            toolInput={message.toolInput}
            toolOutput={message.toolOutput}
            content={isToolResult ? message.content : undefined}
            durationMs={message.durationMs}
            success={message.success}
            outputSize={message.outputSize}
            highlight={highlight}
            expanded={toolExpanded}
            onToggle={() => onToggle(position, 'tool', toolExpanded)}
          />
          <div className="text-xs text-muted-foreground mt-1 ml-1">
            {formatTimestamp(message.timestamp)}
          </div>
        </div>
      </div>
    )
  }

  // Assistant events of OTLP carry tokens and cost, but no content
  const hasMetadataOnly = isAssistant && !message.content && Boolean(message.inputTokens || message.costUsd)
  const tokenInfo = formatTokenUsage({
    input: message.inputTokens,
    output: message.outputTokens,
    cacheRead: message.cacheRead,
    cacheWrite: message.cacheWrite,
  })
  const costInfo = message.costUsd ? formatCost(message.costUsd) : null
  const durationInfo = message.durationMs ? formatDurationMs(message.durationMs) : null

  return (
    <div
      className={cn('group flex gap-3', isUser ? 'justify-end' : 'justify-start')}
      data-position={position}
      data-active={active || undefined}
    >
      {!isUser && (
        <div className="shrink-0 w-8 h-8 rounded-full bg-primary/10 flex items-center justify-center">
          <Bot className="h-4 w-4 text-primary" />
        </div>
      )}

      <div
        className={cn(
          'max-w-[80%] min-w-0 rounded-2xl px-4 py-3 overflow-hidden',
          isUser ? 'bg-primary text-primary-foreground rounded-br-md' : 'bg-muted rounded-bl-md',
          active && 'ring-2 ring-warning ring-offset-2 ring-offset-background',
        )}
      >
        {hasMetadataOnly ? (
          <div className="text-sm text-muted-foreground italic">
            Response content not captured via OTLP telemetry
          </div>
        ) : isAssistant ? (
          <Markdown className="text-sm" highlight={highlight}>
            {message.content}
          </Markdown>
        ) : (
          // User messages as plain text
          <div className="whitespace-pre-wrap text-sm break-all [overflow-wrap:anywhere]">
            <HighlightedText text={message.content} terms={highlight} />
          </div>
        )}

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
            'flex items-center gap-2 text-xs mt-2',
            isUser ? 'text-primary-foreground/70' : 'text-muted-foreground',
          )}
        >
          <span>
            {formatTimestamp(message.timestamp)}
            {message.model && !isUser && <span className="ml-2">{message.model}</span>}
          </span>
          {message.content && (
            <CopyButton
              value={message.content}
              label="Copy message"
              className={cn('-my-1 ml-auto size-6', isUser && 'text-primary-foreground/70 hover:text-primary-foreground hover:bg-primary-foreground/10')}
            />
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
})
