import { useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { DateRangePicker } from '@/components/ui/date-range-picker'
import { Download, Plus, Settings, X } from 'lucide-react'
import { useIsMobile } from '@/hooks/use-mobile'
import { useDashboardStore } from '@/stores/dashboardStore'
import { downloadDashboardExport } from '@/lib/dashboard-export'

export function DashboardToolbar() {
  const isMobile = useIsMobile()
  const {
    dashboard,
    isEditMode,
    setEditMode,
    timeSelection,
    setTimeSelection,
    setAddPanelOpen,
  } = useDashboardStore()

  // Auto-exit edit mode when switching to mobile view
  useEffect(() => {
    if (isMobile && isEditMode) {
      setEditMode(false)
    }
  }, [isMobile, isEditMode, setEditMode])

  return (
    <div className="flex items-center gap-3">
      {/* Date Range Picker */}
      <div className="flex items-center gap-2">
        <label className="text-sm text-muted-foreground whitespace-nowrap hidden sm:inline">Timeframe</label>
        <DateRangePicker
          value={timeSelection}
          onChange={setTimeSelection}
        />
      </div>

      {/* Edit Mode Actions - hidden on mobile */}
      {!isMobile && (
        isEditMode ? (
          <>
            <Button
              variant="outline"
              onClick={() => setAddPanelOpen(true)}
            >
              <Plus className="h-4 w-4 mr-1" />
              <span className="hidden sm:inline">Add Widget</span>
              <span className="sm:hidden">Add</span>
            </Button>
            <Button
              variant="secondary"
              onClick={() => setEditMode(false)}
            >
              <X className="h-4 w-4 mr-1" />
              Done
            </Button>
          </>
        ) : (
          <>
            {dashboard && (
              <Button
                variant="outline"
                onClick={() => downloadDashboardExport(dashboard)}
              >
                <Download className="h-4 w-4 mr-1" />
                Export
              </Button>
            )}
            <Button
              variant="outline"
              onClick={() => setEditMode(true)}
            >
              <Settings className="h-4 w-4 mr-1" />
              Edit
            </Button>
          </>
        )
      )}
    </div>
  )
}
