import type { ReactNode } from 'react'
import { Card, CardContent } from '@/components/ui/card'
import { CHART_COLORS } from '@/components/charts'
import type { BarRow } from '../data'

export function ViewHeader({ title, subtitle, children }: { title: ReactNode; subtitle?: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-base font-semibold leading-tight">{title}</h1>
        {subtitle && <p className="mt-0.5 text-xs text-muted-foreground">{subtitle}</p>}
      </div>
      {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  )
}

export function StatTile({ label, value }: { label: string; value: string }) {
  return (
    <Card className="min-w-36 flex-1 shadow-none">
      <CardContent className="px-4 py-3">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
      </CardContent>
    </Card>
  )
}

export function Notice({ title, text }: { title?: string; text?: string }) {
  return (
    <div className="space-y-1 py-6 text-center text-sm">
      {title && <div className="font-medium">{title}</div>}
      {text && <div className="whitespace-pre-wrap text-muted-foreground">{text}</div>}
    </div>
  )
}

export function ErrorText({ text }: { text?: string }) {
  return text ? (
    <p role="alert" className="text-xs text-error">
      {text}
    </p>
  ) : null
}

// Bars are ranked values of one measure, so they share one color; the
// values are written out, so no axis is needed
const MAX_BARS = 20

export function BarList({ rows, format, label }: { rows: BarRow[]; format: (value: number) => string; label: string }) {
  if (rows.length === 0) {
    return <Notice text="Nothing recorded in this range." />
  }
  const max = Math.max(...rows.map((r) => r.value))
  const shown = rows.slice(0, MAX_BARS)
  return (
    <div className="space-y-1.5">
      <ul className="space-y-1.5" aria-label={label}>
        {shown.map((row) => (
          <li
            key={row.key}
            className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto] items-center gap-3 rounded px-1 py-0.5 text-xs hover:bg-accent"
            title={`${row.label}${row.detail ? ` (${row.detail})` : ''}: ${format(row.value)}`}
          >
            <div className="min-w-0">
              <div className="truncate font-medium">{row.label}</div>
              {row.detail && <div className="truncate text-muted-foreground">{row.detail}</div>}
            </div>
            <div className="h-2.5 rounded-sm bg-muted">
              <div
                className="h-full rounded-sm"
                style={{ width: `${max > 0 ? Math.max((row.value / max) * 100, 0.5) : 0}%`, backgroundColor: CHART_COLORS[0] }}
              />
            </div>
            <div className="text-right tabular-nums">{format(row.value)}</div>
          </li>
        ))}
      </ul>
      {rows.length > shown.length && (
        <p className="px-1 text-xs text-muted-foreground">and {rows.length - shown.length} more</p>
      )}
    </div>
  )
}
