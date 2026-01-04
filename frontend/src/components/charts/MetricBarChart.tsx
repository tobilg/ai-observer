import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import type { Payload } from 'recharts/types/component/DefaultTooltipContent'
import {
  CHART_COLORS,
  compactTooltipStyle,
  standardTooltipStyle,
  compactAxisStyle,
  type MetricChartProps,
} from './chartUtils'

// Custom tooltip component that correctly reads values from the payload
interface CustomTooltipProps {
  active?: boolean
  payload?: Payload<number, string>[]
  label?: string
  tooltipFormatter: (value: number | undefined, name: string | undefined) => [string, string]
  compact?: boolean
}

function CustomTooltip({ active, payload, label, tooltipFormatter, compact }: CustomTooltipProps) {
  if (!active || !payload || payload.length === 0) {
    return null
  }

  const style = compact ? compactTooltipStyle : standardTooltipStyle

  return (
    <div style={style.contentStyle}>
      <p style={style.labelStyle}>{label}</p>
      {payload.map((entry: Payload<number, string>, index: number) => {
        // Read value directly from payload entry which Recharts populates from data[dataKey]
        const value = entry.value as number | undefined
        const name = entry.dataKey as string
        const [formattedValue, displayLabel] = tooltipFormatter(value, name)
        const color = entry.color || entry.fill

        return (
          <p key={`${name}-${index}`} style={{ ...style.itemStyle, color }}>
            {displayLabel} : {formattedValue}
          </p>
        )
      })}
    </div>
  )
}

export function MetricBarChart({
  data,
  series,
  colorMap,
  formatYAxis,
  tooltipFormatter,
  xAxisInterval,
  compact = false,
  showLegend = false,
  stacked = true,
}: MetricChartProps) {
  // Tilt labels when there are many data points (non-compact mode only)
  const needsTiltedLabels = !compact && data.length > 12

  // Check if labels are short (time-only, like "14:30") vs long (date+time)
  const hasShortLabels = data.length > 0 && String(data[0].time || '').length < 10

  // Calculate default x-axis interval for compact mode
  const defaultInterval = compact
    ? Math.max(0, Math.floor(data.length / 4) - 1)
    : 0

  // When labels are tilted, show all labels (interval=0) since they won't overlap
  const interval = needsTiltedLabels ? 0 : (xAxisInterval ?? defaultInterval)

  // Left margin: more for long date labels, less for short time-only labels
  const leftMargin = needsTiltedLabels ? (hasShortLabels ? 15 : 40) : 0

  return (
    <ResponsiveContainer width="100%" height="100%">
      <BarChart
        data={data}
        margin={
          compact
            ? { top: 5, right: 25, left: 0, bottom: 0 }
            : { top: 5, right: 20, left: leftMargin, bottom: 5 }
        }
      >
        <CartesianGrid
          strokeDasharray="3 3"
          stroke={compact ? 'hsl(var(--border))' : undefined}
        />
        <XAxis
          dataKey="time"
          tick={
            {
              fontSize: compact ? 10 : 11,
              angle: needsTiltedLabels ? -45 : 0,
              textAnchor: needsTiltedLabels ? 'end' : 'middle',
              dy: needsTiltedLabels ? 5 : 0,
            } as React.SVGProps<SVGTextElement>
          }
          interval={interval}
          height={needsTiltedLabels ? 90 : undefined}
          {...(compact ? compactAxisStyle : {})}
        />
        <YAxis
          tick={{ fontSize: compact ? 10 : 12 }}
          width={compact ? 50 : undefined}
          tickFormatter={formatYAxis}
          {...(compact ? compactAxisStyle : {})}
        />
        <Tooltip
          offset={compact ? 20 : undefined}
          content={<CustomTooltip tooltipFormatter={tooltipFormatter} compact={compact} />}
        />
        {showLegend && (
          <Legend
            verticalAlign="bottom"
            wrapperStyle={{ paddingTop: needsTiltedLabels ? 20 : 10 }}
            formatter={(value) => {
              const s = series.find((s) => s.key === value)
              return s?.label || value
            }}
          />
        )}
        {series.map((s) => (
          <Bar
            key={s.key}
            dataKey={s.key}
            name={s.key}
            fill={colorMap.get(s.key) || CHART_COLORS[0]}
            isAnimationActive={false}
            barSize={compact ? 8 : 12}
            stackId={stacked ? 'stack' : undefined}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  )
}
