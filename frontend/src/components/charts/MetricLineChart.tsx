import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Legend,
} from 'recharts'
import {
  CHART_COLORS,
  compactTooltipStyle,
  compactAxisStyle,
  type MetricChartProps,
} from './chartUtils'

export function MetricLineChart({
  data,
  series,
  colorMap,
  formatYAxis,
  tooltipFormatter,
  xAxisInterval,
  compact = false,
  showLegend = false,
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
      <LineChart
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
          formatter={tooltipFormatter}
          {...(compact ? compactTooltipStyle : {})}
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
          <Line
            key={s.key}
            type="monotone"
            dataKey={s.key}
            stroke={colorMap.get(s.key) || CHART_COLORS[0]}
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
          />
        ))}
      </LineChart>
    </ResponsiveContainer>
  )
}
