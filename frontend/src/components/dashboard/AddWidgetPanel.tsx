import { useState } from 'react'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { Plus, Save } from 'lucide-react'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useShallow } from 'zustand/react/shallow'
import { useAddWidget, useUpdateWidget } from '@/hooks/useDashboardMutations'
import {
  WIDGET_DEFINITIONS,
  WIDGET_TYPES,
  type CreateWidgetRequest,
  type DashboardWidget,
  type UpdateWidgetRequest,
  type WidgetDefinition,
} from '@/types/dashboard'
import { getMetricMetadata } from '@/lib/metricMetadata'
import { GRID_COLUMNS, nextFreePosition, widgetFitError, type GridPosition } from '@/lib/dashboardGrid'
import { errorText } from '@/lib/errors'
import { cn } from '@/lib/utils'
import { MetricWidgetFields } from './MetricWidgetFields'
import {
  metricFieldsFromConfig,
  metricWidgetConfig,
  useResolvedMetric,
  type MetricFieldsValue,
} from './metricWidgetConfig'

// The server limits titles to 255 characters
const MAX_TITLE_LENGTH = 255
const SIZE_OPTIONS = [1, 2, 3, 4]

function isMetricWidget(widgetType: string): boolean {
  return widgetType === WIDGET_TYPES.METRIC_VALUE || widgetType === WIDGET_TYPES.METRIC_CHART
}

function metricTitle(metric: string): string {
  return getMetricMetadata(metric)?.displayName || metric
}

function columnsLabel(n: number): string {
  return `${n} column${n > 1 ? 's' : ''} wide`
}

interface AddWidgetPanelProps {
  dashboardId: string
  widgets: DashboardWidget[]
}

/**
 * The widget panel of edit mode: adds a widget (at the clicked empty cell or
 * the next free position), or changes one (title, metric settings, size).
 */
export function AddWidgetPanel({ dashboardId, widgets }: AddWidgetPanelProps) {
  const { isAddPanelOpen, targetPosition, editingWidgetId, closePanel } = useDashboardStore(
    useShallow((s) => ({
      isAddPanelOpen: s.isAddPanelOpen,
      targetPosition: s.targetPosition,
      editingWidgetId: s.editingWidgetId,
      closePanel: s.closePanel,
    }))
  )
  const editing = editingWidgetId ? widgets.find((w) => w.id === editingWidgetId) : undefined
  // Owned here, not by the form: the form closes before the request ends,
  // and the callbacks of an unmounted caller do not run
  const updateWidget = useUpdateWidget(dashboardId)
  const saveWidget = (widgetId: string, changes: UpdateWidgetRequest) =>
    updateWidget.mutate(
      { widgetId, changes },
      { onError: (error) => toast.error("Couldn't save the widget", { description: errorText(error) }) }
    )

  return (
    <Sheet open={isAddPanelOpen} onOpenChange={(open) => !open && closePanel()}>
      <SheetContent className="w-full sm:w-135 sm:max-w-[calc(100vw-2rem)] p-6 flex flex-col">
        <SheetHeader className="px-0 pb-4">
          <SheetTitle>{editingWidgetId ? 'Edit Widget' : 'Add Widget'}</SheetTitle>
          <SheetDescription>
            {editingWidgetId ? 'Change the title, data and size of this widget' : 'Choose a widget to add to your dashboard'}
          </SheetDescription>
        </SheetHeader>

        {editing ? (
          // Keyed: the form starts from the widget's stored settings
          <EditWidgetForm key={editing.id} widget={editing} widgets={widgets} onSave={saveWidget} onDone={closePanel} />
        ) : editingWidgetId ? (
          <p className="text-sm text-muted-foreground">This widget no longer exists.</p>
        ) : (
          <AddWidgetForm
            dashboardId={dashboardId}
            widgets={widgets}
            targetPosition={targetPosition}
            onDone={closePanel}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

// ============================================================================
// Adding
// ============================================================================

interface AddWidgetFormProps {
  dashboardId: string
  widgets: DashboardWidget[]
  targetPosition: GridPosition | null
  onDone: () => void
}

function AddWidgetForm({ dashboardId, widgets, targetPosition, onDone }: AddWidgetFormProps) {
  const addWidget = useAddWidget(dashboardId)
  const [fields, setFields] = useState<MetricFieldsValue>(() => metricFieldsFromConfig(undefined))
  const [widgetType, setWidgetType] = useState<string>('')
  const { metric } = useResolvedMetric(fields)

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

  const add = (req: CreateWidgetRequest) => {
    addWidget.mutate(req, {
      onSuccess: onDone,
      onError: (error) => toast.error('Failed to add widget', { description: errorText(error) }),
    })
  }

  const handleAddBuiltinWidget = (definition: WidgetDefinition) => {
    const position = placeWidget(definition.defaultColSpan)
    if (!position) return
    add({
      widgetType: definition.type,
      title: definition.label,
      gridColumn: position.gridColumn,
      gridRow: position.gridRow,
      colSpan: definition.defaultColSpan,
      rowSpan: definition.defaultRowSpan,
    })
  }

  const handleAddMetricWidget = () => {
    const definition = WIDGET_DEFINITIONS.find((d) => d.type === widgetType)
    if (!metric || !definition) return
    const position = placeWidget(definition.defaultColSpan)
    if (!position) return
    add({
      widgetType,
      title: metricTitle(metric),
      gridColumn: position.gridColumn,
      gridRow: position.gridRow,
      colSpan: definition.defaultColSpan,
      rowSpan: definition.defaultRowSpan,
      config: metricWidgetConfig(widgetType, fields, metric),
    })
  }

  const builtinWidgets = WIDGET_DEFINITIONS.filter((d) => d.category === 'builtin')
  const metricWidgets = WIDGET_DEFINITIONS.filter((d) => d.category === 'metrics')
  const adding = addWidget.isPending

  return (
    <Tabs defaultValue="metrics" className="flex-1 flex flex-col overflow-hidden">
      <TabsList className="grid w-full grid-cols-2">
        <TabsTrigger value="metrics">Metrics</TabsTrigger>
        <TabsTrigger value="builtin">Built-in</TabsTrigger>
      </TabsList>

      <TabsContent value="builtin" className="flex-1 mt-4 overflow-hidden">
        <ScrollArea className="h-full">
          <div className="space-y-3 pr-4">
            {builtinWidgets.map((definition) => (
              <Card key={definition.type} className="hover:bg-accent/50 transition-colors">
                <CardHeader className="p-4 pb-2">
                  <div className="flex items-center justify-between">
                    <CardTitle className="text-base">{definition.label}</CardTitle>
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={adding}
                      onClick={() => handleAddBuiltinWidget(definition)}
                      aria-label={`Add ${definition.label}`}
                    >
                      <Plus className="h-4 w-4" />
                    </Button>
                  </div>
                  <CardDescription className="text-xs">{definition.description}</CardDescription>
                </CardHeader>
                <CardContent className="p-4 pt-0">
                  <p className="text-xs text-muted-foreground">{columnsLabel(definition.defaultColSpan)}</p>
                </CardContent>
              </Card>
            ))}
          </div>
        </ScrollArea>
      </TabsContent>

      <TabsContent value="metrics" className="flex-1 mt-4 overflow-auto px-1">
        <div className="space-y-4">
          <MetricWidgetFields idPrefix="add-widget" widgetType={widgetType} value={fields} onChange={setFields} />

          <Separator />

          <div className="space-y-3">
            <span id="add-widget-display-type" className="text-sm font-medium">
              Display Type
            </span>
            <div role="group" aria-labelledby="add-widget-display-type" className="space-y-3">
              {metricWidgets.map((definition) => (
                <button
                  key={definition.type}
                  type="button"
                  aria-pressed={widgetType === definition.type}
                  className={cn(
                    'block w-full text-left cursor-pointer rounded-lg border-2 p-4 transition-colors',
                    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                    widgetType === definition.type ? 'border-primary bg-accent/50' : 'border-border hover:bg-accent/30'
                  )}
                  onClick={() => setWidgetType(definition.type)}
                >
                  <span className="block font-medium">{definition.label}</span>
                  <span className="block text-xs text-muted-foreground mt-1">{definition.description}</span>
                  <span className="block text-xs text-muted-foreground mt-1">{columnsLabel(definition.defaultColSpan)}</span>
                </button>
              ))}
            </div>
          </div>

          <Button className="w-full" disabled={!metric || !widgetType || adding} onClick={handleAddMetricWidget}>
            <Plus className="h-4 w-4 mr-2" />
            Add Widget
          </Button>
        </div>
      </TabsContent>
    </Tabs>
  )
}

// ============================================================================
// Editing
// ============================================================================

interface EditWidgetFormProps {
  widget: DashboardWidget
  widgets: DashboardWidget[]
  // Stores the changes (shown at once, rolled back if the server refuses them)
  onSave: (widgetId: string, changes: UpdateWidgetRequest) => void
  onDone: () => void
}

function EditWidgetForm({ widget, widgets, onSave, onDone }: EditWidgetFormProps) {
  const metricWidget = isMetricWidget(widget.widgetType)
  const [title, setTitle] = useState(widget.title)
  const [titleEdited, setTitleEdited] = useState(false)
  const [fields, setFields] = useState<MetricFieldsValue>(() => metricFieldsFromConfig(widget.config))
  const [colSpan, setColSpan] = useState(widget.colSpan)
  const [rowSpan, setRowSpan] = useState(widget.rowSpan)
  const { metric } = useResolvedMetric(fields)

  // A title not typed by hand follows the chosen metric
  const metricChanged = metricWidget && metric !== (widget.config?.metricName ?? '')
  const shownTitle = metricChanged && !titleEdited && metric ? metricTitle(metric) : title

  // The widget stays where it is; the other widgets must leave room
  const others = widgets.filter((w) => w.id !== widget.id)
  const fitError = widgetFitError(others, { gridRow: widget.gridRow, gridColumn: widget.gridColumn }, colSpan, rowSpan)
  const titleError = !shownTitle.trim()
    ? 'Enter a title'
    : shownTitle.length > MAX_TITLE_LENGTH
      ? `Use at most ${MAX_TITLE_LENGTH} characters`
      : null
  const canSave = !fitError && !titleError && (!metricWidget || Boolean(metric))

  const save = (e: React.FormEvent) => {
    e.preventDefault()
    if (!canSave) return
    onSave(widget.id, {
      title: shownTitle.trim(),
      colSpan,
      rowSpan,
      // The whole configuration: a sent config replaces the stored one
      config: metricWidget ? metricWidgetConfig(widget.widgetType, fields, metric) : (widget.config ?? {}),
    })
    onDone()
  }

  return (
    <form onSubmit={save} className="flex-1 overflow-auto px-1 space-y-4">
      <div className="space-y-2">
        <Label htmlFor="edit-widget-title">Title</Label>
        <Input
          id="edit-widget-title"
          value={shownTitle}
          maxLength={MAX_TITLE_LENGTH}
          onChange={(e) => {
            setTitle(e.target.value)
            setTitleEdited(true)
          }}
          aria-invalid={Boolean(titleError)}
          aria-describedby={titleError ? 'edit-widget-title-error' : undefined}
        />
        {titleError && (
          <p id="edit-widget-title-error" className="text-xs text-destructive">
            {titleError}
          </p>
        )}
      </div>

      {metricWidget && (
        <MetricWidgetFields idPrefix="edit-widget" widgetType={widget.widgetType} value={fields} onChange={setFields} />
      )}

      <Separator />

      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">Size</legend>
        <div className="flex flex-wrap gap-4">
          <div className="space-y-1">
            <Label htmlFor="edit-widget-width" className="text-xs text-muted-foreground">
              Width
            </Label>
            <NativeSelect
              id="edit-widget-width"
              className="h-10"
              value={String(colSpan)}
              onChange={(e) => setColSpan(Number(e.target.value))}
              aria-invalid={Boolean(fitError)}
              aria-describedby={fitError ? 'edit-widget-size-error' : undefined}
            >
              {SIZE_OPTIONS.filter((n) => n <= GRID_COLUMNS).map((n) => (
                <NativeSelectOption key={n} value={n}>
                  {n} column{n > 1 ? 's' : ''}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
          <div className="space-y-1">
            <Label htmlFor="edit-widget-height" className="text-xs text-muted-foreground">
              Height
            </Label>
            <NativeSelect
              id="edit-widget-height"
              className="h-10"
              value={String(rowSpan)}
              onChange={(e) => setRowSpan(Number(e.target.value))}
              aria-invalid={Boolean(fitError)}
              aria-describedby={fitError ? 'edit-widget-size-error' : undefined}
            >
              {SIZE_OPTIONS.map((n) => (
                <NativeSelectOption key={n} value={n}>
                  {n} row{n > 1 ? 's' : ''}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>
        </div>
        <p id="edit-widget-size-error" role={fitError ? 'alert' : undefined} className={cn('text-xs', fitError ? 'text-destructive' : 'text-muted-foreground')}>
          {fitError ?? 'The widget keeps its position; it grows to the right and down.'}
        </p>
      </fieldset>

      <div className="flex gap-2 pt-2">
        <Button type="button" variant="outline" className="flex-1" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" className="flex-1" disabled={!canSave}>
          <Save className="h-4 w-4 mr-2" />
          Save
        </Button>
      </div>
    </form>
  )
}
