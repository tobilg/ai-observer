import { memo, useCallback } from 'react'
import { ChevronDown, ChevronUp, User, Bot, Wrench, CheckCircle, XCircle, Zap, Coins, Clock, FileText } from 'lucide-react'
import { cn, formatTimestamp } from '@/lib/utils'
import { formatBytes, formatCost, formatDurationMs, formatTokenUsage } from '@/lib/format'
import { prettyJSON } from '@/lib/transcript'
import { useExpansion } from '@/hooks/useExpansion'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/common/copy-button'
import { HighlightedText } from '@/components/common/highlighted-text'
import { Markdown } from '@/components/common/markdown'
import { CodeBlock } from '@/components/sessions/CodeBlock'
import { TranscriptList } from './TranscriptList'
import type { TranscriptViewProps } from './types'
import type { TranscriptMessage } from '@/types/sessions'

// Height of an item before it is measured
const ESTIMATED_HEIGHT = 140

// Longer content is shortened until expanded
const PREVIEW_CHARS = 200

// Theme tokens where one fits (info, success, warning); tool results keep
// a colour of their own
function getRoleConfig(role: string) {
  switch (role) {
    case 'user':
      return { icon: User, label: 'User', color: 'bg-info/10 text-info border-info/20', lineColor: 'bg-info' }
    case 'assistant':
      return { icon: Bot, label: 'Assistant', color: 'bg-success/10 text-success border-success/20', lineColor: 'bg-success' }
    case 'tool_use':
      return { icon: Wrench, label: 'Tool Call', color: 'bg-warning/10 text-warning border-warning/20', lineColor: 'bg-warning' }
    case 'tool_result':
      return {
        icon: CheckCircle,
        label: 'Tool Result',
        color: 'bg-purple-500/10 text-purple-500 border-purple-500/20',
        lineColor: 'bg-purple-500',
      }
    default:
      return { icon: Bot, label: role, color: 'bg-muted text-muted-foreground border-muted', lineColor: 'bg-muted-foreground' }
  }
}

export function TimelineView({ messages, positions, highlight, activePosition, revealKey, className }: TranscriptViewProps) {
  const { isExpanded, toggle } = useExpansion()
  const positionAt = useCallback((index: number) => positions?.[index] ?? index, [positions])
  const activeIndex = activePosition === undefined ? undefined : (positions ? positions.indexOf(activePosition) : activePosition)

  const renderItem = (index: number) => {
    const position = positionAt(index)
    const active = position === activePosition
    return (
      <TimelineItem
        message={messages[index]}
        position={position}
        isLast={index === messages.length - 1}
        active={active}
        highlight={highlight}
        contentExpanded={isExpanded(position, 'content', active)}
        inputExpanded={isExpanded(position, 'input', active)}
        outputExpanded={isExpanded(position, 'output', active)}
        onToggle={toggle}
      />
    )
  }

  return (
    <TranscriptList
      className={className}
      count={messages.length}
      itemKey={positionAt}
      renderItem={renderItem}
      estimateSize={ESTIMATED_HEIGHT}
      gapClassName=""
      revealIndex={activeIndex}
      revealKey={revealKey}
    />
  )
}

interface TimelineItemProps {
  message: TranscriptMessage
  position: number
  isLast: boolean
  // The current search match
  active: boolean
  highlight?: readonly string[]
  contentExpanded: boolean
  inputExpanded: boolean
  outputExpanded: boolean
  onToggle: (position: number, part: string, current: boolean) => void
}

function DisclosureButton({ expanded, onClick, children }: { expanded: boolean; onClick: () => void; children: string }) {
  return (
    <button
      type="button"
      className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
      aria-expanded={expanded}
      onClick={onClick}
    >
      {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
      {children}
    </button>
  )
}

const TimelineItem = memo(function TimelineItem({
  message, position, isLast, active, highlight, contentExpanded, inputExpanded, outputExpanded, onToggle,
}: TimelineItemProps) {
  const config = getRoleConfig(message.role)
  const Icon = config.icon

  const hasExpandableContent = Boolean(message.content) && message.content.length > PREVIEW_CHARS
  const displayContent = contentExpanded || !hasExpandableContent
    ? message.content
    : message.content.slice(0, PREVIEW_CHARS) + '...'

  const hasToolDetails = message.toolName || message.toolInput
  const isToolRole = message.role === 'tool_use' || message.role === 'tool_result'
  const isAssistant = message.role === 'assistant'

  const tokenInfo = formatTokenUsage({
    input: message.inputTokens,
    output: message.outputTokens,
    cacheRead: message.cacheRead,
    cacheWrite: message.cacheWrite,
  })
  const costInfo = message.costUsd ? formatCost(message.costUsd) : null
  const durationInfo = message.durationMs ? formatDurationMs(message.durationMs) : null
  const hasMetadata = Boolean(tokenInfo || costInfo || durationInfo)

  // Assistant events of OTLP carry tokens and cost, but no content
  const hasMetadataOnly = isAssistant && !message.content && hasMetadata

  return (
    <div className="relative flex gap-4" data-position={position} data-active={active || undefined}>
      {/* Timeline line */}
      {!isLast && (
        <div className={cn('absolute left-[15px] top-10 bottom-0 w-0.5', config.lineColor, 'opacity-30')} />
      )}

      {/* Timeline dot */}
      <div className={cn('shrink-0 w-8 h-8 rounded-full flex items-center justify-center border', config.color)}>
        <Icon className="h-4 w-4" />
      </div>

      {/* Content */}
      <div className={cn('flex-1 pb-6 min-w-0', active && 'rounded-lg ring-2 ring-warning ring-offset-4 ring-offset-background')}>
        <div className="flex items-center gap-2 mb-1 flex-wrap">
          <Badge variant="outline" className={cn('text-xs', config.color)}>
            {config.label}
          </Badge>
          <span className="text-xs text-muted-foreground">{formatTimestamp(message.timestamp)}</span>
          {message.model && <span className="text-xs text-muted-foreground">{message.model}</span>}
          {isToolRole && message.success !== undefined && (
            message.success ? (
              <CheckCircle className="h-3.5 w-3.5 text-success" aria-label="Succeeded" />
            ) : (
              <XCircle className="h-3.5 w-3.5 text-error" aria-label="Failed" />
            )
          )}
          {isToolRole && message.durationMs !== undefined && message.durationMs > 0 && (
            <Badge variant="secondary" className="text-xs py-0 h-5">
              <Clock className="h-3 w-3 mr-1" />
              {formatDurationMs(message.durationMs)}
            </Badge>
          )}
          {message.content && (
            <CopyButton value={message.content} label="Copy message" className="-my-1 size-6" />
          )}
        </div>

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

        {hasToolDetails && (
          <div className="mb-2 space-y-1">
            {message.toolName && (
              <span className="font-mono text-sm font-medium">
                <HighlightedText text={message.toolName} terms={highlight} />
              </span>
            )}
            {message.toolInput && (
              <div>
                <DisclosureButton expanded={inputExpanded} onClick={() => onToggle(position, 'input', inputExpanded)}>
                  View input
                </DisclosureButton>
                {inputExpanded && (
                  <CodeBlock
                    className="mt-1"
                    text={prettyJSON(message.toolInput)}
                    maxHeightClassName="max-h-48 overflow-y-auto"
                    highlight={highlight}
                    copyLabel="Copy tool input"
                  />
                )}
              </div>
            )}
            {/* Tool output (from imports) */}
            {message.toolOutput && (
              <div>
                <DisclosureButton expanded={outputExpanded} onClick={() => onToggle(position, 'output', outputExpanded)}>
                  View output
                </DisclosureButton>
                {outputExpanded && (
                  <CodeBlock
                    className="mt-1"
                    text={message.toolOutput}
                    maxHeightClassName="max-h-96 overflow-y-auto"
                    highlight={highlight}
                    copyLabel="Copy tool output"
                  />
                )}
              </div>
            )}
            {/* Output size indicator when no actual output */}
            {!message.toolOutput && message.outputSize !== undefined && message.outputSize > 0 && (
              <div className="text-xs text-muted-foreground flex items-center gap-1">
                <FileText className="h-3 w-3" />
                Output not captured via OTLP ({formatBytes(message.outputSize)})
              </div>
            )}
          </div>
        )}

        {hasMetadataOnly ? (
          <div className="bg-muted/50 rounded-lg p-3">
            <span className="text-sm text-muted-foreground italic">
              Response content not captured via OTLP telemetry
            </span>
          </div>
        ) : message.content ? (
          <div className="bg-muted/50 rounded-lg p-3 w-full max-w-full min-w-0 overflow-x-auto">
            {isAssistant ? (
              <Markdown className="text-sm" highlight={highlight}>
                {displayContent || ''}
              </Markdown>
            ) : (
              <pre className="text-sm whitespace-pre-wrap font-sans max-w-full min-w-0 break-all [overflow-wrap:anywhere]">
                <HighlightedText text={displayContent} terms={highlight} />
              </pre>
            )}
            {hasExpandableContent && (
              <Button
                variant="ghost"
                size="sm"
                className="mt-2 h-6 text-xs"
                aria-expanded={contentExpanded}
                onClick={() => onToggle(position, 'content', contentExpanded)}
              >
                {contentExpanded ? (
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
})
