import { useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { DateRangePicker } from '@/components/common/date-range-picker'
import { Download, Plus, Settings, X } from 'lucide-react'
import { useIsMobile } from '@/hooks/use-mobile'
import { useDashboardStore } from '@/stores/dashboardStore'
import { useShallow } from 'zustand/react/shallow'
import { downloadDashboardExport } from '@/lib/dashboard-export'
import type { DashboardWithWidgets, TimeSelection } from '@/types/dashboard'

interface DashboardToolbarProps {
  dashboard: DashboardWithWidgets | undefined
  timeSelection: TimeSelection
  onTimeSelectionChange: (selection: TimeSelection) => void
  // Leaves edit mode (closing empty rows)
  onDone: () => void
}

export function DashboardToolbar({ dashboard, timeSelection, onTimeSelectionChange, onDone }: DashboardToolbarProps) {
  const isMobile = useIsMobile()
  const { isEditMode, setEditMode, openAddPanel } = useDashboardStore(
    useShallow((s) => ({
      isEditMode: s.isEditMode,
      setEditMode: s.setEditMode,
      openAddPanel: s.openAddPanel,
    }))
  )

  // Editing needs the desktop grid: leave edit mode on small screens
  useEffect(() => {
    if (isMobile && isEditMode) {
      onDone()
    }
  }, [isMobile, isEditMode, onDone])

  return (
    <div className="flex flex-wrap items-center gap-2 sm:gap-3">
      <div className="flex min-w-0 items-center gap-2">
        <Label htmlFor="dashboard-timeframe" className="text-sm font-normal text-muted-foreground whitespace-nowrap hidden sm:inline">
          Timeframe
        </Label>
        <DateRangePicker id="dashboard-timeframe" label="Timeframe" value={timeSelection} onChange={onTimeSelectionChange} />
      </div>

      {/* Editing is only offered on the desktop grid */}
      {!isMobile && (
        isEditMode ? (
          <>
            <Button variant="outline" onClick={() => openAddPanel()}>
              <Plus className="h-4 w-4 mr-1" />
              Add Widget
            </Button>
            <Button variant="secondary" onClick={onDone}>
              <X className="h-4 w-4 mr-1" />
              Done
            </Button>
          </>
        ) : (
          <>
            {dashboard && (
              <Button variant="outline" onClick={() => downloadDashboardExport(dashboard)}>
                <Download className="h-4 w-4 mr-1" />
                Export
              </Button>
            )}
            <Button variant="outline" onClick={() => setEditMode(true)} disabled={!dashboard}>
              <Settings className="h-4 w-4 mr-1" />
              Edit
            </Button>
          </>
        )
      )}
    </div>
  )
}
