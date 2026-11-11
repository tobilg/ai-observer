import { memo, useState } from 'react'
import { ChevronDown, ChevronUp, Wrench, CheckCircle, XCircle, Clock, FileText } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { HighlightedText } from '@/components/common/highlighted-text'
import { CodeBlock } from '@/components/sessions/CodeBlock'
import { formatBytes, formatDurationMs } from '@/lib/format'
import { prettyJSON } from '@/lib/transcript'

interface ToolCallBlockProps {
  toolName: string
  toolInput?: string
  toolOutput?: string
  content?: string
  durationMs?: number
  success?: boolean
  outputSize?: number
  // Search terms to mark
  highlight?: readonly string[]
  // Controlled expansion (virtualized lists keep it while the row unmounts);
  // without it the block keeps its own
  expanded?: boolean
  onToggle?: () => void
  className?: string
}

export const ToolCallBlock = memo(function ToolCallBlock({
  toolName,
  toolInput,
  toolOutput,
  content,
  durationMs,
  success,
  outputSize,
  highlight,
  expanded,
  onToggle,
  className,
}: ToolCallBlockProps) {
  const [ownExpanded, setOwnExpanded] = useState(false)
  const isExpanded = expanded ?? ownExpanded

  const formattedInput = toolInput ? prettyJSON(toolInput) : undefined
  // Use toolOutput (from imports) if available, otherwise fall back to content
  const outputContent = toolOutput || content
  const hasDetails = Boolean(formattedInput || outputContent || outputSize)

  const toggle = () => {
    if (!hasDetails) return
    if (onToggle) {
      onToggle()
    } else {
      setOwnExpanded((e) => !e)
    }
  }

  return (
    <div className={cn('rounded-lg border bg-muted/30 w-full max-w-full min-w-0 overflow-hidden', className)}>
      <Button
        variant="ghost"
        className="w-full justify-between h-auto py-2 px-3 hover:bg-muted/50"
        onClick={toggle}
        disabled={!hasDetails}
        aria-expanded={hasDetails ? isExpanded : undefined}
      >
        <div className="flex items-center gap-2 text-sm">
          <Wrench className="h-4 w-4 text-muted-foreground" />
          <span className="font-medium">
            <HighlightedText text={toolName} terms={highlight} />
          </span>
          {success !== undefined && (
            success ? (
              <CheckCircle className="h-3.5 w-3.5 text-success" aria-label="Succeeded" />
            ) : (
              <XCircle className="h-3.5 w-3.5 text-error" aria-label="Failed" />
            )
          )}
          {durationMs !== undefined && durationMs > 0 && (
            <Badge variant="secondary" className="text-xs py-0 h-5">
              <Clock className="h-3 w-3 mr-1" />
              {formatDurationMs(durationMs)}
            </Badge>
          )}
        </div>
        {hasDetails && (
          isExpanded ? (
            <ChevronUp className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          )
        )}
      </Button>

      {isExpanded && hasDetails && (
        <div className="px-3 pb-3 space-y-3 min-w-0 overflow-hidden">
          {formattedInput && (
            <div>
              <div className="text-xs font-medium text-muted-foreground mb-1">Input</div>
              <CodeBlock
                text={formattedInput}
                maxHeightClassName="max-h-48 overflow-y-auto"
                highlight={highlight}
                copyLabel="Copy tool input"
              />
            </div>
          )}
          {outputContent ? (
            <div>
              <div className="text-xs font-medium text-muted-foreground mb-1">Output</div>
              <CodeBlock
                text={outputContent}
                maxHeightClassName="max-h-96 overflow-y-auto"
                highlight={highlight}
                copyLabel="Copy tool output"
              />
            </div>
          ) : outputSize !== undefined && outputSize > 0 ? (
            <div>
              <div className="text-xs font-medium text-muted-foreground mb-1">Output</div>
              <div className="text-xs text-muted-foreground bg-muted rounded p-2 flex items-center gap-2">
                <FileText className="h-3.5 w-3.5" />
                <span>Output not captured via OTLP telemetry ({formatBytes(outputSize)})</span>
              </div>
            </div>
          ) : null}
        </div>
      )}
    </div>
  )
})
