import { useState, useEffect, useMemo } from 'react'
import { NavLink, useNavigate, useLocation } from 'react-router-dom'
import {
  Plus,
  MoreVertical,
  Star,
  Pencil,
  Trash2,
} from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuAction,
  useSidebar,
} from '@/components/ui/sidebar'
import { useDashboardStore } from '@/stores/dashboardStore'
import { DashboardDialog } from '@/components/dashboard/DashboardDialog'
import { DeleteDashboardDialog } from '@/components/dashboard/DeleteDashboardDialog'
import { toast } from 'sonner'
import type { Dashboard } from '@/types/dashboard'
import type { DashboardExport } from '@/types/dashboard-export'

export function SidebarDashboards() {
  const navigate = useNavigate()
  const location = useLocation()
  const { setOpenMobile } = useSidebar()

  // Dialog states
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedDashboard, setSelectedDashboard] = useState<Dashboard | null>(null)

  const {
    dashboards,
    dashboardsLoading,
    loadDashboards,
    createNewDashboard,
    importDashboard,
    renameDashboard,
    deleteDashboardById,
    setAsDefault,
  } = useDashboardStore()

  // Load dashboards on mount
  useEffect(() => {
    loadDashboards()
  }, [loadDashboards])

  // Sort dashboards: default first, then alphabetically by name
  const sortedDashboards = useMemo(() => {
    return [...dashboards].sort((a, b) => {
      // Default dashboard always first
      if (a.isDefault) return -1
      if (b.isDefault) return 1
      // Rest sorted alphabetically by name (case-insensitive)
      return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
    })
  }, [dashboards])

  // Get current dashboard ID from URL
  const currentDashboardId = location.pathname.startsWith('/dashboard/')
    ? location.pathname.split('/')[2]
    : null

  const handleNavigate = (path: string) => {
    navigate(path)
    setOpenMobile(false) // Close mobile sidebar on navigation
  }

  const handleCreate = async (name: string, description?: string) => {
    try {
      const dashboard = await createNewDashboard(name, description)
      toast.success('Dashboard created')
      handleNavigate(`/dashboard/${dashboard.id}`)
    } catch {
      toast.error('Failed to create dashboard')
      throw new Error('Failed to create dashboard')
    }
  }

  const handleImport = async (data: DashboardExport) => {
    try {
      const dashboard = await importDashboard(data)
      toast.success(`Dashboard "${dashboard.name}" imported`)
      handleNavigate(`/dashboard/${dashboard.id}`)
    } catch {
      toast.error('Failed to import dashboard')
      throw new Error('Failed to import dashboard')
    }
  }

  const handleRename = async (name: string) => {
    if (!selectedDashboard) return
    try {
      await renameDashboard(selectedDashboard.id, name)
      toast.success('Dashboard renamed')
    } catch {
      toast.error('Failed to rename dashboard')
      throw new Error('Failed to rename dashboard')
    }
  }

  const handleDelete = async () => {
    if (!selectedDashboard) return
    try {
      await deleteDashboardById(selectedDashboard.id)
      toast.success('Dashboard deleted')
      // Navigate to default dashboard if we deleted the current one
      if (currentDashboardId === selectedDashboard.id) {
        handleNavigate('/')
      }
    } catch {
      toast.error('Failed to delete dashboard')
      throw new Error('Failed to delete dashboard')
    }
  }

  const handleSetDefault = async (dashboard: Dashboard) => {
    try {
      await setAsDefault(dashboard.id)
      toast.success(`"${dashboard.name}" is now the default dashboard`)
    } catch {
      toast.error('Failed to set default dashboard')
    }
  }

  const openEditDialog = (dashboard: Dashboard) => {
    setSelectedDashboard(dashboard)
    setEditDialogOpen(true)
  }

  const openDeleteDialog = (dashboard: Dashboard) => {
    setSelectedDashboard(dashboard)
    setDeleteDialogOpen(true)
  }

  return (
    <>
      <SidebarMenu>
        {dashboardsLoading ? (
          <div className="px-2 py-1 text-xs text-muted-foreground">Loading...</div>
        ) : sortedDashboards.length === 0 ? (
          <div className="px-2 py-1 text-xs text-muted-foreground">No dashboards</div>
        ) : (
          sortedDashboards.map((dashboard) => {
            // Determine the route for this dashboard
            const dashboardPath = dashboard.isDefault ? '/' : `/dashboard/${dashboard.id}`
            const isActive = dashboard.isDefault
              ? location.pathname === '/' && !currentDashboardId
              : currentDashboardId === dashboard.id

            return (
              <SidebarMenuItem key={dashboard.id} className="group/dashboard">
                <SidebarMenuButton
                  asChild
                  isActive={isActive}
                >
                  <NavLink
                    to={dashboardPath}
                    onClick={() => setOpenMobile(false)}
                  >
                    {dashboard.isDefault ? (
                      <Star className="h-4 w-4 fill-current text-yellow-500" />
                    ) : (
                      <span className="h-4 w-4" />
                    )}
                    <span className="truncate">{dashboard.name}</span>
                  </NavLink>
                </SidebarMenuButton>

                {/* Context menu */}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <SidebarMenuAction
                      showOnHover
                      className="opacity-0 group-hover/dashboard:opacity-100"
                    >
                      <MoreVertical className="h-4 w-4" />
                      <span className="sr-only">Dashboard options</span>
                    </SidebarMenuAction>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-48">
                    <DropdownMenuItem onClick={() => openEditDialog(dashboard)}>
                      <Pencil className="mr-2 h-4 w-4" />
                      Rename
                    </DropdownMenuItem>
                    {!dashboard.isDefault && (
                      <DropdownMenuItem onClick={() => handleSetDefault(dashboard)}>
                        <Star className="mr-2 h-4 w-4" />
                        Set as Default
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onClick={() => openDeleteDialog(dashboard)}
                      className="text-destructive focus:text-destructive"
                      disabled={dashboard.isDefault}
                    >
                      <Trash2 className="mr-2 h-4 w-4" />
                      Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </SidebarMenuItem>
            )
          })
        )}

        {/* Add dashboard button */}
        <SidebarMenuItem>
          <SidebarMenuButton
            onClick={() => setCreateDialogOpen(true)}
          >
            <Plus className="h-4 w-4" />
            <span>New Dashboard</span>
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>

      {/* Dialogs */}
      <DashboardDialog
        open={createDialogOpen}
        onOpenChange={setCreateDialogOpen}
        mode="create"
        onSubmit={handleCreate}
        onImport={handleImport}
      />

      <DashboardDialog
        open={editDialogOpen}
        onOpenChange={setEditDialogOpen}
        mode="edit"
        initialName={selectedDashboard?.name || ''}
        onSubmit={handleRename}
      />

      <DeleteDashboardDialog
        open={deleteDialogOpen}
        onOpenChange={setDeleteDialogOpen}
        dashboardName={selectedDashboard?.name || ''}
        onConfirm={handleDelete}
      />
    </>
  )
}
