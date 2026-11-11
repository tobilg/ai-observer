import { useState, useMemo } from 'react'
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
  SidebarMenuSkeleton,
  useSidebar,
} from '@/components/ui/sidebar'
import { DashboardDialog } from '@/components/dashboard/DashboardDialog'
import { DeleteDashboardDialog } from '@/components/dashboard/DeleteDashboardDialog'
import { useDashboards } from '@/lib/queries'
import { errorText } from '@/lib/errors'
import {
  useCreateDashboard,
  useDeleteDashboard,
  useImportDashboard,
  useSetDefaultDashboard,
  useUpdateDashboard,
} from '@/hooks/useDashboardMutations'
import { toast } from 'sonner'
import type { Dashboard } from '@/types/dashboard'
import type { DashboardExport } from '@/types/dashboard-export'

export function SidebarDashboards() {
  const navigate = useNavigate()
  const location = useLocation()
  const { isMobile, setOpenMobile } = useSidebar()

  // Dialog states
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedDashboard, setSelectedDashboard] = useState<Dashboard | null>(null)

  const dashboardsQuery = useDashboards()
  const createDashboard = useCreateDashboard()
  const importDashboard = useImportDashboard()
  const updateDashboard = useUpdateDashboard()
  const deleteDashboard = useDeleteDashboard()
  const setDefaultDashboard = useSetDefaultDashboard()

  // Sort dashboards: default first, then alphabetically by name
  const sortedDashboards = useMemo(() => {
    return [...(dashboardsQuery.data ?? [])].sort((a, b) => {
      // Default dashboard always first
      if (a.isDefault) return -1
      if (b.isDefault) return 1
      // Rest sorted alphabetically by name (case-insensitive)
      return a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
    })
  }, [dashboardsQuery.data])

  // Get current dashboard ID from URL
  const currentDashboardId = location.pathname.startsWith('/dashboard/')
    ? decodeURIComponent(location.pathname.split('/')[2] ?? '')
    : null

  const closeOnMobile = () => {
    if (isMobile) setOpenMobile(false)
  }

  const handleNavigate = (path: string) => {
    navigate(path)
    closeOnMobile()
  }

  // The dialogs stay open (and say nothing) when these throw
  const handleCreate = async (name: string, description?: string) => {
    try {
      const dashboard = await createDashboard.mutateAsync({ name, description })
      toast.success('Dashboard created')
      handleNavigate(`/dashboard/${dashboard.id}`)
    } catch (error) {
      toast.error('Failed to create dashboard', { description: errorText(error) })
      throw error
    }
  }

  const handleImport = async (data: DashboardExport) => {
    try {
      const dashboard = await importDashboard.mutateAsync(data)
      toast.success(`Dashboard "${dashboard.name}" imported`)
      handleNavigate(`/dashboard/${dashboard.id}`)
    } catch (error) {
      toast.error('Failed to import dashboard', { description: errorText(error) })
      throw error
    }
  }

  const handleRename = async (name: string) => {
    if (!selectedDashboard) return
    try {
      await updateDashboard.mutateAsync({ id: selectedDashboard.id, changes: { name } })
      toast.success('Dashboard renamed')
    } catch (error) {
      toast.error('Failed to rename dashboard', { description: errorText(error) })
      throw error
    }
  }

  const handleDelete = async () => {
    if (!selectedDashboard) return
    try {
      await deleteDashboard.mutateAsync(selectedDashboard.id)
      toast.success('Dashboard deleted')
      // Navigate to default dashboard if we deleted the current one
      if (currentDashboardId === selectedDashboard.id) {
        handleNavigate('/')
      }
    } catch (error) {
      toast.error('Failed to delete dashboard', { description: errorText(error) })
      throw error
    }
  }

  const handleSetDefault = (dashboard: Dashboard) => {
    setDefaultDashboard.mutate(dashboard.id, {
      onSuccess: () => toast.success(`"${dashboard.name}" is now the default dashboard`),
      onError: (error) => toast.error('Failed to set default dashboard', { description: errorText(error) }),
    })
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
        {dashboardsQuery.isPending ? (
          <>
            <SidebarMenuSkeleton />
            <SidebarMenuSkeleton />
          </>
        ) : dashboardsQuery.isError ? (
          <div role="alert" className="px-2 py-1 text-xs text-destructive">
            {errorText(dashboardsQuery.error, "Couldn't load the dashboards")}{' '}
            <button
              type="button"
              className="underline underline-offset-2"
              onClick={() => dashboardsQuery.refetch()}
            >
              Retry
            </button>
          </div>
        ) : sortedDashboards.length === 0 ? (
          <div className="px-2 py-1 text-xs text-muted-foreground">No dashboards</div>
        ) : (
          sortedDashboards.map((dashboard) => {
            // The default dashboard lives at "/"; it is also current at its own address
            const dashboardPath = dashboard.isDefault ? '/' : `/dashboard/${dashboard.id}`
            const isActive = currentDashboardId
              ? currentDashboardId === dashboard.id
              : dashboard.isDefault && location.pathname === '/'

            return (
              <SidebarMenuItem key={dashboard.id} className="group/dashboard">
                <SidebarMenuButton
                  asChild
                  isActive={isActive}
                >
                  <NavLink
                    to={dashboardPath}
                    onClick={closeOnMobile}
                    aria-current={isActive ? 'page' : undefined}
                  >
                    {dashboard.isDefault ? (
                      <Star className="h-4 w-4 fill-current text-yellow-500" aria-label="Default dashboard" />
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
                      className="opacity-0 group-hover/dashboard:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"
                    >
                      <MoreVertical className="h-4 w-4" />
                      <span className="sr-only">Options of {dashboard.name}</span>
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
