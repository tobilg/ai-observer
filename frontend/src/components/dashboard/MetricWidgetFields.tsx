import { useMemo } from 'react'
import { BarChart3, Layers } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { MetricOptions } from '@/components/common/metric-options'
import { useServiceNames } from '@/hooks/useServiceNames'
import {
  HISTOGRAM_STATS,
  breakdownOptions,
  defaultHistogramStat,
  getMetricMetadata,
  isAdditiveStat,
  isHistogramMetric,
} from '@/lib/metricMetadata'
import { groupMetricsBySource } from '@/lib/metricGroups'
import { useBreakdownValues, useMetricAttributeKeys, useServices } from '@/lib/queries'
import { WIDGET_TYPES } from '@/types/dashboard'
import { useResolvedMetric, type MetricFieldsValue } from './metricWidgetConfig'

interface MetricWidgetFieldsProps {
  // Prefix of the controls' ids (labels point at them)
  idPrefix: string
  // The widget type the fields are for ('' while none is chosen)
  widgetType: string
  value: MetricFieldsValue
  onChange: (value: MetricFieldsValue) => void
}

// Service, metric, breakdown, statistic (histograms) and chart mode of a
// metric widget
export function MetricWidgetFields({ idPrefix, widgetType, value, onChange }: MetricWidgetFieldsProps) {
  const services = useServices()
  const { filterOptions } = useServiceNames()
  const { metric, names } = useResolvedMetric(value)
  const metadata = useMemo(() => (metric ? getMetricMetadata(metric) : null), [metric])
  const groupedMetrics = useMemo(() => groupMetricsBySource(names, value.service), [names, value.service])
  const breakdownValues = useBreakdownValues(
    { name: metric, attribute: value.breakdown, service: value.service || undefined },
    { enabled: widgetType === WIDGET_TYPES.METRIC_VALUE },
  )
  const attributeKeys = useMetricAttributeKeys({ name: metric, service: value.service || undefined })

  const isValue = widgetType === WIDGET_TYPES.METRIC_VALUE
  const isChart = widgetType === WIDGET_TYPES.METRIC_CHART
  // The catalog's breakdowns, then the other attributes the points have
  const breakdowns = useMemo(() => {
    const options = breakdownOptions(metadata, attributeKeys.data)
    // A stored breakdown stays selectable before the keys have loaded
    if (value.breakdown && !options.some((o) => o.value === value.breakdown)) {
      options.push({ value: value.breakdown, label: value.breakdown, known: false })
    }
    return options
  }, [metadata, attributeKeys.data, value.breakdown])
  const breakdown = metadata?.breakdowns?.find((b) => b.attributeKey === value.breakdown)
  const histogram = isHistogramMetric(metadata)
  // Averages, extremes and percentiles do not add up, so they are not stacked
  const stackable = !metadata || !histogram || isAdditiveStat(value.stat || defaultHistogramStat(metadata))

  const set = (changes: Partial<MetricFieldsValue>) => onChange({ ...value, ...changes })

  return (
    <div className="space-y-4">
      <div className="space-y-2 *:w-full">
        <Label htmlFor={`${idPrefix}-service`}>Service (optional)</Label>
        <NativeSelect
          id={`${idPrefix}-service`}
          className="h-10"
          value={value.service}
          onChange={(e) => set({ service: e.target.value })}
        >
          <NativeSelectOption value="">All Services</NativeSelectOption>
          {filterOptions(services.data ?? []).map((o) => (
            <NativeSelectOption key={o.value} value={o.value}>
              {o.label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>

      <div className="space-y-2 *:w-full">
        <Label htmlFor={`${idPrefix}-metric`}>Metric</Label>
        <NativeSelect
          id={`${idPrefix}-metric`}
          className="h-10"
          value={metric}
          onChange={(e) => set({ metric: e.target.value, breakdown: '', breakdownValue: '', stat: '' })}
        >
          <NativeSelectOption value="">Select a metric</NativeSelectOption>
          <MetricOptions groups={groupedMetrics} />
        </NativeSelect>
      </div>

      {metadata && (
        <Card className="bg-muted/50">
          <CardContent className="p-3 space-y-2">
            <div className="flex items-center gap-2">
              <span className="font-medium text-sm">{metadata.displayName}</span>
              {metadata.unit.displayUnit && (
                <Badge variant="outline" className="text-xs">
                  {metadata.unit.displayUnit}
                </Badge>
              )}
            </div>
            <p className="text-xs text-muted-foreground">{metadata.description}</p>
          </CardContent>
        </Card>
      )}

      {breakdowns.length > 0 && (isChart || isValue) && (
        <>
          <div className="space-y-2 *:w-full">
            <Label htmlFor={`${idPrefix}-breakdown`}>Breakdown By {isValue ? '' : '(optional)'}</Label>
            <NativeSelect
              id={`${idPrefix}-breakdown`}
              className="h-10"
              value={value.breakdown}
              onChange={(e) => set({ breakdown: e.target.value, breakdownValue: '' })}
              aria-describedby={`${idPrefix}-breakdown-hint`}
            >
              <NativeSelectOption value="">{isValue ? 'Total (all values)' : 'Default (type)'}</NativeSelectOption>
              {breakdowns.map((b) => (
                <NativeSelectOption key={b.value} value={b.value}>
                  {b.label}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <p id={`${idPrefix}-breakdown-hint`} className="text-xs text-muted-foreground">
              {isValue
                ? 'Select an attribute to filter by a specific value'
                : 'Choose which attribute to use for multi-series breakdown'}
            </p>
          </div>

          {isValue && value.breakdown && (
            <div className="space-y-2 *:w-full">
              <Label htmlFor={`${idPrefix}-breakdown-value`}>Value</Label>
              <NativeSelect
                id={`${idPrefix}-breakdown-value`}
                className="h-10"
                value={value.breakdownValue}
                onChange={(e) => set({ breakdownValue: e.target.value })}
                disabled={breakdownValues.isLoading}
              >
                <NativeSelectOption value="">{breakdownValues.isLoading ? 'Loading...' : 'Select a value'}</NativeSelectOption>
                {/* Keeps a stored value selectable while the values load */}
                {value.breakdownValue && !(breakdownValues.data ?? []).includes(value.breakdownValue) && (
                  <NativeSelectOption value={value.breakdownValue}>
                    {breakdown?.knownValues?.[value.breakdownValue] || value.breakdownValue}
                  </NativeSelectOption>
                )}
                {(breakdownValues.data ?? []).map((v) => (
                  <NativeSelectOption key={v} value={v}>
                    {breakdown?.knownValues?.[v] || v}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              <p className="text-xs text-muted-foreground">
                {breakdownValues.isError ? "Couldn't load the values of this attribute" : 'Show only this specific value'}
              </p>
            </div>
          )}
        </>
      )}

      {metadata && histogram && (isChart || isValue) && (
        <div className="space-y-2 *:w-full">
          <Label htmlFor={`${idPrefix}-stat`}>Statistic</Label>
          <NativeSelect
            id={`${idPrefix}-stat`}
            className="h-10"
            value={value.stat}
            onChange={(e) => set({ stat: e.target.value })}
            aria-describedby={`${idPrefix}-stat-hint`}
          >
            <NativeSelectOption value="">
              Default ({HISTOGRAM_STATS.find((s) => s.value === defaultHistogramStat(metadata))?.label})
            </NativeSelectOption>
            {HISTOGRAM_STATS.map((s) => (
              <NativeSelectOption key={s.value} value={s.value}>
                {s.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <p id={`${idPrefix}-stat-hint`} className="text-xs text-muted-foreground">
            A histogram records a distribution: show its average, a percentile, extremes, sum or count
          </p>
        </div>
      )}

      {isChart && (
        <div className="space-y-2">
          <span id={`${idPrefix}-chart-mode`} className="text-sm font-medium leading-none">
            Chart Mode
          </span>
          <div role="group" aria-labelledby={`${idPrefix}-chart-mode`} className="flex border rounded-md w-fit">
            <Button
              type="button"
              variant={value.stacked && stackable ? 'secondary' : 'ghost'}
              size="sm"
              className="h-8 px-3 rounded-r-none gap-2"
              aria-pressed={value.stacked && stackable}
              disabled={!stackable}
              onClick={() => set({ stacked: true })}
            >
              <Layers className="h-4 w-4" aria-hidden="true" />
              Stacked
            </Button>
            <Button
              type="button"
              variant={!value.stacked || !stackable ? 'secondary' : 'ghost'}
              size="sm"
              className="h-8 px-3 rounded-l-none gap-2"
              aria-pressed={!value.stacked || !stackable}
              onClick={() => set({ stacked: false })}
            >
              <BarChart3 className="h-4 w-4" aria-hidden="true" />
              Grouped
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            {stackable ? 'Choose how to display multiple series' : 'This statistic does not add up: its series are shown side by side'}
          </p>
        </div>
      )}
    </div>
  )
}
