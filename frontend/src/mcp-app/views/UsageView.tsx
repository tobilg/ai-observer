import { useState } from 'react'
import { MetricBarChart } from '@/components/charts'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatCurrency, formatNumber } from '@/lib/metricMetadata'
import { formatRange, usageBars, usageDaily, type UsageMeasure } from '../data'
import type { CallTool, UsageGroup, UsageSummary } from '../types'
import { BarList, ErrorText, Notice, StatTile, ViewHeader } from './parts'

const GROUPS: Array<{ value: UsageGroup; label: string }> = [
  { value: 'tool', label: 'Tool' },
  { value: 'model', label: 'Model' },
  { value: 'day', label: 'Day' },
  { value: 'repository', label: 'Repository' },
]

function formatTokens(value: number): string {
  return formatNumber(Math.round(value))
}

// usage_summary: totals, then bars per tool, model or repository, or daily
// bars stacked by tool. Another grouping calls the tool again for the same
// range.
export function UsageView({ data, args, callTool }: { data: UsageSummary; args: Record<string, unknown>; callTool?: CallTool }) {
  const [summary, setSummary] = useState(data)
  const [measure, setMeasure] = useState<UsageMeasure>('cost')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string>()

  const regroup = async (groupBy: UsageGroup) => {
    if (!callTool) return
    setPending(true)
    setError(undefined)
    try {
      // The range the first call resolved, so ages such as 7d do not move
      setSummary((await callTool('usage_summary', { ...args, from: summary.from, to: summary.to, group_by: groupBy })) as UsageSummary)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setPending(false)
    }
  }

  const format = measure === 'cost' ? formatCurrency : formatTokens
  const totalTokens = summary.rows.reduce((sum, row) => sum + row.total_tokens, 0)
  const daily = summary.group_by === 'day' ? usageDaily(summary, measure) : undefined

  return (
    <div className="space-y-4">
      <ViewHeader title="Usage" subtitle={formatRange(summary.from, summary.to)}>
        <Tabs value={measure} onValueChange={(value) => setMeasure(value as UsageMeasure)}>
          <TabsList>
            <TabsTrigger value="cost">Cost</TabsTrigger>
            <TabsTrigger value="tokens">Tokens</TabsTrigger>
          </TabsList>
        </Tabs>
      </ViewHeader>

      <div className="flex flex-wrap gap-3">
        <StatTile label="Cost" value={formatCurrency(summary.total_cost_usd)} />
        <StatTile label="Tokens" value={formatTokens(totalTokens)} />
      </div>

      <Tabs value={summary.group_by} onValueChange={(value) => void regroup(value as UsageGroup)}>
        <TabsList aria-label="Group by">
          {GROUPS.map((group) => (
            <TabsTrigger key={group.value} value={group.value} disabled={pending || (!callTool && group.value !== summary.group_by)}>
              {group.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <ErrorText text={error} />

      <div className={pending ? 'opacity-50 transition-opacity' : undefined} aria-busy={pending}>
        {measure === 'cost' && totalTokens > 0 && summary.total_cost_usd === 0 ? (
          <Notice text="Token usage was recorded with no associated cost in this range." />
        ) : daily ? (
          daily.data.length === 0 ? (
            <Notice text="Nothing recorded in this range." />
          ) : (
            <div className="h-64">
              <MetricBarChart
                data={daily.data}
                series={daily.series}
                colorMap={daily.colorMap}
                formatYAxis={(value) => (measure === 'cost' ? `$${formatNumber(value)}` : formatNumber(value))}
                tooltipFormatter={(value, name) => [
                  value === undefined ? '—' : format(value),
                  daily.series.find((s) => s.key === name)?.label ?? name ?? '',
                ]}
                showLegend={daily.series.length > 1}
                compact={daily.data.length > 12}
              />
            </div>
          )
        ) : (
          <BarList rows={usageBars(summary, measure)} format={format} label={`${measure === 'cost' ? 'Cost' : 'Tokens'} by ${summary.group_by}`} />
        )}
      </div>

      {summary.unpriced_models?.length ? (
        <p className="text-xs text-muted-foreground">
          No price, so not in the cost: {summary.unpriced_models.join(', ')}
        </p>
      ) : null}
      <p className="text-[11px] leading-snug text-muted-foreground">{summary.note}</p>
    </div>
  )
}
