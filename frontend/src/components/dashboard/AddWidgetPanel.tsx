import { useEffect, useState, useMemo, useCallback } from 'react'
import { toast } from 'sonner'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Plus, Layers, BarChart3 } from 'lucide-react'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useShallow } from 'zustand/react/shallow'
import { api } from '@/lib/api'
import {
  WIDGET_DEFINITIONS,
  type CreateWidgetRequest,
  type WidgetDefinition,
} from '@/types/dashboard'
import { getMetricMetadata, getServiceDisplayName } from '@/lib/metricMetadata'
import { groupMetricsBySource } from '@/lib/metricGroups'
import { nextFreePosition, widgetFitError } from '@/lib/dashboardGrid'
import { MetricOptions } from '@/components/common/metric-options'

export function AddWidgetPanel() {
  const {
    isAddPanelOpen,
    setAddPanelOpen,
    widgets,
    addWidget,
    targetPosition,
    setTargetPosition,
  } = useDashboardStore(
    useShallow((s) => ({
      isAddPanelOpen: s.isAddPanelOpen,
      setAddPanelOpen: s.setAddPanelOpen,
      widgets: s.widgets,
      addWidget: s.addWidget,
      targetPosition: s.targetPosition,
      setTargetPosition: s.setTargetPosition,
    }))
  )
  const [services, setServices] = useState<string[]>([])
  const [metricNames, setMetricNames] = useState<string[]>([])
  const [selectedService, setSelectedService] = useState<string>('')
  const [selectedMetric, setSelectedMetric] = useState<string>('')
  const [selectedWidgetType, setSelectedWidgetType] = useState<string>('')
  const [selectedBreakdown, setSelectedBreakdown] = useState<string>('')
  const [selectedBreakdownValue, setSelectedBreakdownValue] = useState<string>('')
  const [breakdownValues, setBreakdownValues] = useState<string[]>([])
  const [loadingBreakdownValues, setLoadingBreakdownValues] = useState(false)
  const [chartStacked, setChartStacked] = useState(true)
  const [adding, setAdding] = useState(false)

  // Get metadata for the selected metric
  const selectedMetadata = useMemo(
    () => (selectedMetric ? getMetricMetadata(selectedMetric) : null),
    [selectedMetric]
  )

  // Group metric names by source
  const groupedMetrics = useMemo(
    () => groupMetricsBySource(metricNames, selectedService),
    [metricNames, selectedService]
  )

  const handleMetricChange = useCallback((metricName: string) => {
    setSelectedMetric(metricName)
    setSelectedBreakdown('')
    setSelectedBreakdownValue('')
    setBreakdownValues([])
  }, [])

  const handleBreakdownChange = useCallback((breakdown: string) => {
    setSelectedBreakdown(breakdown)
    setSelectedBreakdownValue('')
    setBreakdownValues([])
  }, [])

  // Fetch breakdown values when attribute is selected
  useEffect(() => {
    if (!selectedMetric || !selectedBreakdown) {
      return
    }

    const fetchBreakdownValues = async () => {
      setLoadingBreakdownValues(true)
      try {
        const response = await api.getBreakdownValues({
          name: selectedMetric,
          attribute: selectedBreakdown,
          service: selectedService || undefined,
        })
        setBreakdownValues(response.values || [])
      } catch (error) {
        console.error('Failed to fetch breakdown values:', error)
        setBreakdownValues([])
      } finally {
        setLoadingBreakdownValues(false)
      }
    }

    fetchBreakdownValues()
  }, [selectedMetric, selectedBreakdown, selectedService])

  // Fetch services on mount
  useEffect(() => {
    const fetchServices = async () => {
      try {
        const data = await api.getServices()
        setServices(data.services ?? [])
      } catch (error) {
        console.error('Failed to fetch services:', error)
      }
    }
    fetchServices()
  }, [])

  // Fetch metric names when service changes
  useEffect(() => {
    const fetchMetricNames = async () => {
      try {
        const data = await api.getMetricNames(selectedService || undefined)
        setMetricNames(data.names ?? [])
        if (data.names?.length > 0 && !selectedMetric) {
          handleMetricChange(data.names[0])
        }
      } catch (error) {
        console.error('Failed to fetch metric names:', error)
      }
    }
    fetchMetricNames()
  }, [selectedService, selectedMetric, handleMetricChange])

  // The position of a new widget, or null (after telling why) when the chosen
  // position has too few free columns
  const placeWidget = (colSpan: number) => {
    if (!targetPosition) {
      return nextFreePosition(widgets)
    }
    const fitError = widgetFitError(widgets, targetPosition, colSpan)
    if (fitError) {
      toast.error('Widget does not fit', { description: fitError })
      return null
    }
    return targetPosition
  }

  const handleAddBuiltinWidget = async (definition: WidgetDefinition) => {
    const position = placeWidget(definition.defaultColSpan)
    if (!position) return

    setAdding(true)
    try {
      const req: CreateWidgetRequest = {
        widgetType: definition.type,
        title: definition.label,
        gridColumn: position.gridColumn,
        gridRow: position.gridRow,
        colSpan: definition.defaultColSpan,
        rowSpan: definition.defaultRowSpan,
      }
      await addWidget(req)
      setTargetPosition(null)
      setAddPanelOpen(false)
    } catch (error) {
      console.error('Failed to add widget:', error)
      toast.error('Failed to add widget')
    } finally {
      setAdding(false)
    }
  }

  const handleAddMetricWidget = async () => {
    if (!selectedMetric || !selectedWidgetType) return

    const definition = WIDGET_DEFINITIONS.find((d) => d.type === selectedWidgetType)
    if (!definition) return

    const position = placeWidget(definition.defaultColSpan)
    if (!position) return

    setAdding(true)
    try {
      // Use display name as title if available
      const title = selectedMetadata?.displayName || selectedMetric
      const req: CreateWidgetRequest = {
        widgetType: selectedWidgetType,
        title,
        gridColumn: position.gridColumn,
        gridRow: position.gridRow,
        colSpan: definition.defaultColSpan,
        rowSpan: definition.defaultRowSpan,
        config: {
          service: selectedService || undefined,
          metricName: selectedMetric,
          breakdownAttribute: selectedBreakdown || undefined,
          breakdownValue: selectedWidgetType === 'metric_value' && selectedBreakdownValue
            ? selectedBreakdownValue
            : undefined,
          chartStacked: selectedWidgetType === 'metric_chart' ? chartStacked : undefined,
        },
      }
      await addWidget(req)
      setTargetPosition(null)
      setAddPanelOpen(false)
      // Reset selections
      setSelectedWidgetType('')
      setSelectedBreakdown('')
      setSelectedBreakdownValue('')
      setBreakdownValues([])
      setChartStacked(true)
    } catch (error) {
      console.error('Failed to add widget:', error)
      toast.error('Failed to add widget')
    } finally {
      setAdding(false)
    }
  }

  const builtinWidgets = WIDGET_DEFINITIONS.filter((d) => d.category === 'builtin')
  const metricWidgets = WIDGET_DEFINITIONS.filter((d) => d.category === 'metrics')

  const handleOpenChange = (open: boolean) => {
    setAddPanelOpen(open)
    if (!open) {
      setTargetPosition(null)
    }
  }

  return (
    <Sheet open={isAddPanelOpen} onOpenChange={handleOpenChange}>
      <SheetContent className="w-100 sm:w-135 p-6 flex flex-col">
        <SheetHeader className="px-0 pb-4">
          <SheetTitle>Add Widget</SheetTitle>
          <SheetDescription>
            Choose a widget to add to your dashboard
          </SheetDescription>
        </SheetHeader>

        <Tabs defaultValue="metrics" className="flex-1 flex flex-col overflow-hidden">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="metrics">Metrics</TabsTrigger>
            <TabsTrigger value="builtin">Built-in</TabsTrigger>
          </TabsList>

          <TabsContent value="builtin" className="flex-1 mt-4 overflow-hidden">
            <ScrollArea className="h-full">
              <div className="space-y-3 pr-4">
                {builtinWidgets.map((definition) => (
                  <Card key={definition.type} className="cursor-pointer hover:bg-accent/50 transition-colors">
                    <CardHeader className="p-4 pb-2">
                      <div className="flex items-center justify-between">
                        <CardTitle className="text-base">{definition.label}</CardTitle>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={adding}
                          onClick={() => handleAddBuiltinWidget(definition)}
                        >
                          <Plus className="h-4 w-4" />
                        </Button>
                      </div>
                      <CardDescription className="text-xs">
                        {definition.description}
                      </CardDescription>
                    </CardHeader>
                    <CardContent className="p-4 pt-0">
                      <p className="text-xs text-muted-foreground">
                        {definition.defaultColSpan} column{definition.defaultColSpan > 1 ? 's' : ''} wide
                      </p>
                    </CardContent>
                  </Card>
                ))}
              </div>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="metrics" className="flex-1 mt-4 overflow-auto px-1">
            <div className="space-y-4">
              {/* Service Filter */}
              <div>
                <label className="text-sm font-medium mb-2 block">Service (optional)</label>
                <Select
                  value={selectedService}
                  onChange={(e) => setSelectedService(e.target.value)}
                >
                  <option value="">All Services</option>
                  {services.map((s) => (
                    <option key={s} value={s}>
                      {getServiceDisplayName(s)}
                    </option>
                  ))}
                </Select>
              </div>

              {/* Metric Name */}
              <div>
                <label className="text-sm font-medium mb-2 block">Metric</label>
                <Select
                  value={selectedMetric}
                  onChange={(e) => handleMetricChange(e.target.value)}
                >
                  <option value="">Select a metric</option>
                  <MetricOptions groups={groupedMetrics} />
                </Select>
              </div>

              {/* Metric Info */}
              {selectedMetadata && (
                <Card className="bg-muted/50">
                  <CardContent className="p-3 space-y-2">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm">{selectedMetadata.displayName}</span>
                      {selectedMetadata.unit.displayUnit && (
                        <Badge variant="outline" className="text-xs">
                          {selectedMetadata.unit.displayUnit}
                        </Badge>
                      )}
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {selectedMetadata.description}
                    </p>
                  </CardContent>
                </Card>
              )}

              {/* Breakdown Attribute (for metric widgets with breakdowns) */}
              {selectedMetadata?.breakdowns && selectedMetadata.breakdowns.length > 0 &&
               (selectedWidgetType === 'metric_chart' || selectedWidgetType === 'metric_value') && (
                <>
                  <div>
                    <label className="text-sm font-medium mb-2 block">
                      Breakdown By {selectedWidgetType === 'metric_value' ? '' : '(optional)'}
                    </label>
                    <Select
                      value={selectedBreakdown}
                      onChange={(e) => handleBreakdownChange(e.target.value)}
                    >
                      <option value="">
                        {selectedWidgetType === 'metric_value' ? 'Total (all values)' : 'Default (type)'}
                      </option>
                      {selectedMetadata.breakdowns.map((breakdown) => (
                        <option key={breakdown.attributeKey} value={breakdown.attributeKey}>
                          {breakdown.displayName}
                        </option>
                      ))}
                    </Select>
                    <p className="text-xs text-muted-foreground mt-1">
                      {selectedWidgetType === 'metric_value'
                        ? 'Select an attribute to filter by a specific value'
                        : 'Choose which attribute to use for multi-series breakdown'}
                    </p>
                  </div>

                  {/* Breakdown Value dropdown - only for metric_value when attribute is selected */}
                  {selectedWidgetType === 'metric_value' && selectedBreakdown && (
                    <div>
                      <label className="text-sm font-medium mb-2 block">Value</label>
                      <Select
                        value={selectedBreakdownValue}
                        onChange={(e) => setSelectedBreakdownValue(e.target.value)}
                        disabled={loadingBreakdownValues}
                      >
                        <option value="">
                          {loadingBreakdownValues ? 'Loading...' : 'Select a value'}
                        </option>
                        {breakdownValues.map((value) => {
                          // Use knownValues mapping if available
                          const breakdown = selectedMetadata.breakdowns?.find(
                            (b) => b.attributeKey === selectedBreakdown
                          )
                          const displayValue = breakdown?.knownValues?.[value] || value
                          return (
                            <option key={value} value={value}>
                              {displayValue}
                            </option>
                          )
                        })}
                      </Select>
                      <p className="text-xs text-muted-foreground mt-1">
                        Show only this specific value
                      </p>
                    </div>
                  )}
                </>
              )}

              {/* Chart Mode (only for chart widgets) */}
              {selectedWidgetType === 'metric_chart' && (
                <div>
                  <label className="text-sm font-medium mb-2 block">Chart Mode</label>
                  <div className="flex border rounded-md w-fit">
                    <Button
                      type="button"
                      variant={chartStacked ? "secondary" : "ghost"}
                      size="sm"
                      className="h-8 px-3 rounded-r-none gap-2"
                      onClick={() => setChartStacked(true)}
                    >
                      <Layers className="h-4 w-4" />
                      Stacked
                    </Button>
                    <Button
                      type="button"
                      variant={!chartStacked ? "secondary" : "ghost"}
                      size="sm"
                      className="h-8 px-3 rounded-l-none gap-2"
                      onClick={() => setChartStacked(false)}
                    >
                      <BarChart3 className="h-4 w-4" />
                      Grouped
                    </Button>
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    Choose how to display multiple series
                  </p>
                </div>
              )}

              <Separator />

              {/* Widget Type Selection */}
              <div className="space-y-4">
                <label className="text-sm font-medium">Display Type</label>
                <div className="space-y-3">
                  {metricWidgets.map((definition) => (
                    <div
                      key={definition.type}
                      className={`cursor-pointer rounded-lg border-2 p-4 transition-colors ${
                        selectedWidgetType === definition.type
                          ? 'border-primary bg-accent/50'
                          : 'border-border hover:bg-accent/30'
                      }`}
                      onClick={() => setSelectedWidgetType(definition.type)}
                    >
                      <p className="font-medium">{definition.label}</p>
                      <p className="text-xs text-muted-foreground mt-1">
                        {definition.description}
                      </p>
                      <p className="text-xs text-muted-foreground mt-1">
                        {definition.defaultColSpan} column{definition.defaultColSpan > 1 ? 's' : ''} wide
                      </p>
                    </div>
                  ))}
                </div>
              </div>

              {/* Add Button */}
              <Button
                className="w-full"
                disabled={!selectedMetric || !selectedWidgetType || adding}
                onClick={handleAddMetricWidget}
              >
                <Plus className="h-4 w-4 mr-2" />
                Add Widget
              </Button>
            </div>
          </TabsContent>
        </Tabs>
      </SheetContent>
    </Sheet>
  )
}
