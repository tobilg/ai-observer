import { createContext, useContext } from 'react'
import type { TimeSeries } from '@/types/metrics'

export interface MetricData {
  series: TimeSeries[]
  loading: boolean
  error: string | null
}

export interface MetricDataContextValue {
  getMetricData: (widgetId: string) => MetricData
  refreshAll: () => void
}

export const MetricDataContext = createContext<MetricDataContextValue | null>(null)

export function useMetricData(widgetId: string): MetricData {
  const context = useContext(MetricDataContext)
  if (!context) {
    throw new Error('useMetricData must be used within MetricDataProvider')
  }
  return context.getMetricData(widgetId)
}
