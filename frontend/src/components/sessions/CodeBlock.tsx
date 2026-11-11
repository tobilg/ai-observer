import { memo, useMemo } from 'react'
import { CopyButton } from '@/components/common/copy-button'
import { HighlightedText } from '@/components/common/highlighted-text'
import { parseArrowNumberedLines } from '@/lib/codeLines'
import { cn } from '@/lib/utils'

interface CodeBlockProps {
  text: string
  className?: string
  maxHeightClassName?: string
  // Search terms to mark
  highlight?: readonly string[]
  // Accessible name of a copy button for the text, e.g. "Copy tool input"
  copyLabel?: string
}

// Tool input and output: preformatted, line numbers of file reads
// ("12→foo") in a column of their own
export const CodeBlock = memo(function CodeBlock({ text, className, maxHeightClassName, highlight, copyLabel }: CodeBlockProps) {
  const { lines, hasLineNumbers } = useMemo(() => parseArrowNumberedLines(text), [text])

  return (
    <div className={cn('relative w-full max-w-full min-w-0', className)}>
      <div className={cn('w-full max-w-full min-w-0 overflow-x-hidden', maxHeightClassName)}>
        {hasLineNumbers ? (
          <div className={cn('text-xs font-mono bg-muted rounded p-2', copyLabel && 'pr-9')}>
            {lines.map((l, idx) => (
              <div key={idx} className="flex items-start">
                <span className="w-[4ch] pr-3 text-muted-foreground select-none text-right tabular-nums shrink-0">
                  {l.lineNumber ?? ''}
                </span>
                <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">
                  <HighlightedText text={l.content} terms={highlight} />
                </span>
              </div>
            ))}
          </div>
        ) : (
          <pre className={cn('text-xs bg-muted rounded p-2 whitespace-pre-wrap break-words min-w-0', copyLabel && 'pr-9')}>
            <HighlightedText text={text} terms={highlight} />
          </pre>
        )}
      </div>
      {copyLabel && <CopyButton value={text} label={copyLabel} className="absolute right-1 top-1 size-7" />}
    </div>
  )
})
