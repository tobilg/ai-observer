import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/queries'
import type { AdminDeleteRequest } from '@/types/status'

// Mutations of the admin API (AI_OBSERVER_ADMIN_API=true). They are never
// retried: the server may have applied them before failing.

/** Deletes telemetry, or with dryRun counts it; a deletion refetches all queries */
export function useAdminDelete() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (req: AdminDeleteRequest) => api.adminDelete(req),
    onSuccess: (response) => {
      if (!response.dryRun) {
        // Every list, chart and count may have changed
        void queryClient.invalidateQueries()
      }
    },
  })
}

/** Checkpoints the database; refetches the status, whose sizes change */
export function useAdminCheckpoint() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.adminCheckpoint(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.status }),
  })
}

/** Writes a backup to the server's backup directory */
export function useAdminBackup() {
  return useMutation({
    mutationFn: (name?: string) => api.adminBackup(name),
  })
}
