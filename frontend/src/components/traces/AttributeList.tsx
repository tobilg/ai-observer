import { memo, useMemo, useState } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/common/copy-button'
import { HighlightedText } from '@/components/common/highlighted-text'
import { formatCount } from '@/lib/format'
import { formatAttributeValue, isLongValue, valuePreview } from './waterfall'

interface AttributeListProps {
  attributes: Record<string, string>
  // Terms to highlight (the span filter)
  terms?: readonly string[]
}

// The attributes of a span, event, link or resource, sorted by key. Values
// are shown whole: wrapped, JSON pretty-printed, long ones shortened until
// expanded, each with a copy button.
export const AttributeList = memo(function AttributeList({ attributes, terms }: AttributeListProps) {
  const entries = useMemo(
    () => Object.entries(attributes).sort(([a], [b]) => a.localeCompare(b)),
    [attributes],
  )
  return (
    <dl className="grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-[minmax(8rem,16rem)_minmax(0,1fr)]">
      {entries.map(([name, value]) => (
        <AttributeRow key={name} name={name} value={value} terms={terms} />
      ))}
    </dl>
  )
})

function AttributeRow({ name, value, terms }: { name: string; value: string; terms?: readonly string[] }) {
  const [expanded, setExpanded] = useState(false)
  const { text } = useMemo(() => formatAttributeValue(value), [value])
  const long = isLongValue(text)
  const shown = long && !expanded ? valuePreview(text) : text

  return (
    <>
      <dt className="break-all text-muted-foreground">
        <HighlightedText text={name} terms={terms} />
      </dt>
      <dd className="min-w-0">
        <div className="flex items-start gap-1">
          <pre className="min-w-0 flex-1 whitespace-pre-wrap break-words font-mono [overflow-wrap:anywhere]">
            <HighlightedText text={shown} terms={terms} />
          </pre>
          <CopyButton value={value} label={`Copy ${name}`} className="-my-1 size-6 shrink-0" />
        </div>
        {long && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 px-1 text-xs text-muted-foreground"
            aria-expanded={expanded}
            onClick={() => setExpanded((e) => !e)}
          >
            {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
            {expanded ? 'Show less' : `Show all (${formatCount(text.length)} characters)`}
          </Button>
        )}
      </dd>
    </>
  )
}
