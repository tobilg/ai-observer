import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MetricValueWidget } from '../MetricValueWidget'
import { MetricDataContext, type MetricData } from '@/contexts/metricData'
import type { WidgetConfig } from '@/types/dashboard'

function renderWidget(data: Partial<MetricData>, config: WidgetConfig) {
  const value = {
    getMetricData: () => ({ series: [], loading: false, refreshing: false, error: null, ...data }),
    refreshAll: () => {},
  }
  return render(
    <MetricDataContext.Provider value={value}>
      <MetricValueWidget widgetId="w1" title="Value" config={config} />
    </MetricDataContext.Provider>
  )
}

describe('MetricValueWidget', () => {
  it('shows the one value the server merged, filtered to the breakdown value', () => {
    renderWidget(
      { series: [{ name: 'claude_code.token.usage', datapoints: [[0, 2500]] }] },
      { metricName: 'claude_code.token.usage', breakdownAttribute: 'type', breakdownValue: 'output' }
    )
    expect(screen.getByText('2.50K')).toBeInTheDocument()
    expect(screen.getByText('(Output)')).toBeInTheDocument()
  })

  it('shows a histogram statistic in its unit, and a count as a number', () => {
    const latency = 'gemini_cli.api.request.latency'
    const { unmount } = renderWidget({ stat: 'p99', series: [{ name: latency, datapoints: [[0, 1830]] }] }, { metricName: latency })
    expect(screen.getByText('1.83s')).toBeInTheDocument()
    expect(screen.getByText('(p99)')).toBeInTheDocument()
    unmount()

    renderWidget({ stat: 'count', series: [{ name: latency, datapoints: [[0, 42]] }] }, { metricName: latency })
    expect(screen.getByText('42')).toBeInTheDocument()
  })

  it('shows a dash without a value', () => {
    const latency = 'gemini_cli.api.request.latency'
    const { unmount } = renderWidget({ stat: 'avg', series: [{ name: latency, datapoints: [[0, null]] }] }, { metricName: latency })
    expect(screen.getByText('—')).toBeInTheDocument()
    unmount()

    renderWidget({ series: [] }, { metricName: latency })
    expect(screen.getByText('—')).toBeInTheDocument()
  })
})
