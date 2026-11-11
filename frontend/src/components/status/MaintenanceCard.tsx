import { useId, useState } from 'react'
import { DatabaseBackup, Eraser, HardDrive, Search } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Separator } from '@/components/ui/separator'
import { CopyButton } from '@/components/common/copy-button'
import { errorText } from '@/lib/errors'
import { formatBytes, formatCount } from '@/lib/format'
import { useProviders } from '@/lib/queries'
import { localDayRange } from '@/lib/status'
import type { AdminDeleteResponse, DeleteScope, StatusSettings } from '@/types/status'
import { useAdminBackup, useAdminCheckpoint, useAdminDelete } from './useMaintenance'

const SCOPES: { value: DeleteScope; label: string }[] = [
  { value: 'all', label: 'All telemetry' },
  { value: 'traces', label: 'Traces (spans)' },
  { value: 'logs', label: 'Logs' },
  { value: 'metrics', label: 'Metric points' },
]

/**
 * Maintenance of the database while the server runs, through the admin API
 * (AI_OBSERVER_ADMIN_API=true): delete a time range, checkpoint, back up.
 * Without the admin API it says how to enable it and which commands do the
 * same with the server stopped.
 */
export function MaintenanceCard({ settings }: { settings: StatusSettings }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Maintenance</CardTitle>
        <CardDescription>
          Delete old telemetry, free its space for reuse and back up the database while the server runs
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {settings.adminApiEnabled ? (
          <>
            <DeleteSection />
            <Separator />
            <div className="grid gap-6 md:grid-cols-2">
              <CheckpointSection />
              <BackupSection backupDir={settings.backupDir} />
            </div>
            <p className="text-sm text-muted-foreground">
              Compaction, which shrinks the database file to its data, rewrites the file and needs the server stopped: run{' '}
              <code>ai-observer compact</code>.
            </p>
          </>
        ) : (
          <div className="space-y-2 text-sm">
            <p>
              The admin API is off. Start the server with <code>AI_OBSERVER_ADMIN_API=true</code> to delete telemetry, run
              checkpoints and write backups from here. It is off by default because it deletes data and writes files; it is
              protected like the rest of the API (<code>AI_OBSERVER_AUTH_TOKEN</code>).
            </p>
            <p className="text-muted-foreground">
              With the server stopped, the commands <code>ai-observer delete</code>, <code>backup</code>, <code>compact</code>{' '}
              and <code>restore</code> do the same.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function DeleteSection() {
  const id = useId()
  const providers = useProviders()
  const [fromDay, setFromDay] = useState('')
  const [toDay, setToDay] = useState('')
  const [service, setService] = useState('')
  const [scope, setScope] = useState<DeleteScope>('all')
  const [confirming, setConfirming] = useState(false)
  // The counts of the preview, for the selection they were counted for
  const [preview, setPreview] = useState<{ key: string; counts: AdminDeleteResponse } | null>(null)
  const [deleted, setDeleted] = useState<AdminDeleteResponse | null>(null)
  const preview$ = useAdminDelete()
  const delete$ = useAdminDelete()

  const range = localDayRange(fromDay, toDay)
  const key = JSON.stringify([range, service, scope])
  const counts = preview?.key === key ? preview.counts : null
  const total = counts ? counts.logs + counts.metrics + counts.spans : 0
  const request = range ? { ...range, service: service || undefined, scope } : null

  const runPreview = () => {
    if (!request) return
    setDeleted(null)
    preview$.mutate({ ...request, dryRun: true }, { onSuccess: (response) => setPreview({ key, counts: response }) })
  }
  const runDelete = () => {
    if (!request) return
    delete$.mutate(request, {
      onSuccess: (response) => {
        setDeleted(response)
        setPreview(null)
        setConfirming(false)
      },
      onError: () => setConfirming(false),
    })
  }

  return (
    <section aria-labelledby={`${id}-title`} className="space-y-4">
      <div>
        <h3 id={`${id}-title`} className="flex items-center gap-2 font-medium">
          <Eraser className="h-4 w-4" aria-hidden="true" />
          Delete telemetry
        </h3>
        <p className="text-sm text-muted-foreground">
          Whole local days, like <code>ai-observer delete</code>. Deleted a day per transaction, so ingestion goes on.
        </p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-from`}>From</Label>
          <Input id={`${id}-from`} type="date" value={fromDay} onChange={(e) => setFromDay(e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-to`}>To</Label>
          <Input id={`${id}-to`} type="date" value={toDay} onChange={(e) => setToDay(e.target.value)} />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-service`}>Tool</Label>
          <NativeSelect id={`${id}-service`} value={service} onChange={(e) => setService(e.target.value)} className="w-full">
            <NativeSelectOption value="">All tools</NativeSelectOption>
            {(providers.data?.providers ?? []).map((p) => (
              <NativeSelectOption key={p.tool} value={p.tool}>
                {p.displayName}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <div className="space-y-1.5">
          <Label htmlFor={`${id}-scope`}>What</Label>
          <NativeSelect
            id={`${id}-scope`}
            value={scope}
            onChange={(e) => setScope(e.target.value as DeleteScope)}
            className="w-full"
          >
            {SCOPES.map((s) => (
              <NativeSelectOption key={s.value} value={s.value}>
                {s.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
      </div>
      {fromDay && toDay && !range && <p className="text-sm text-destructive">The first day must not be after the last.</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" onClick={runPreview} disabled={!range || preview$.isPending}>
          <Search className="h-4 w-4 mr-2" aria-hidden="true" />
          {preview$.isPending ? 'Counting…' : 'Count records'}
        </Button>
        <Button variant="destructive" onClick={() => setConfirming(true)} disabled={!counts || total === 0 || delete$.isPending}>
          {delete$.isPending ? 'Deleting…' : 'Delete…'}
        </Button>
      </div>
      {preview$.isError && <ErrorAlert error={preview$.error} fallback="Counting the records failed." />}
      {delete$.isError && <ErrorAlert error={delete$.error} fallback="Deleting failed." />}
      {counts && (
        <p className="text-sm" role="status">
          {total === 0 ? 'No records in this range.' : `Would delete ${describeCounts(counts)}.`}
        </p>
      )}
      {deleted && (
        <p className="text-sm" role="status">
          Deleted {describeCounts(deleted)}.
          {deleted.database &&
            ` The database file keeps its size (${formatBytes(deleted.database.sizeBytes)}); ${formatBytes(deleted.database.freeBytes)} of it are now free blocks that new data reuses.`}
        </p>
      )}

      <AlertDialog open={confirming} onOpenChange={(open) => !delete$.isPending && setConfirming(open)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete telemetry?</AlertDialogTitle>
            <AlertDialogDescription>
              {counts && `This deletes ${describeCounts(counts)} from ${fromDay} to ${toDay}`}
              {service ? ` of ${providers.data?.providers.find((p) => p.tool === service)?.displayName ?? service}` : ''}.
              This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={delete$.isPending}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                // Keep the dialog open until the deletion is done
                e.preventDefault()
                runDelete()
              }}
              disabled={delete$.isPending}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {delete$.isPending ? 'Deleting…' : 'Delete'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}

function CheckpointSection() {
  const checkpoint = useAdminCheckpoint()
  const db = checkpoint.data?.database
  return (
    <section className="space-y-3">
      <div>
        <h3 className="flex items-center gap-2 font-medium">
          <HardDrive className="h-4 w-4" aria-hidden="true" />
          Checkpoint
        </h3>
        <p className="text-sm text-muted-foreground">
          Writes the write-ahead log into the file and frees the blocks of deleted data for reuse. Deletions run one already.
        </p>
      </div>
      <Button variant="outline" onClick={() => checkpoint.mutate()} disabled={checkpoint.isPending}>
        {checkpoint.isPending ? 'Running…' : 'Run checkpoint'}
      </Button>
      {checkpoint.isError && <ErrorAlert error={checkpoint.error} fallback="The checkpoint failed." />}
      {db && (
        <p className="text-sm" role="status">
          Done: {formatBytes(db.sizeBytes)} file, {formatBytes(db.usedBytes)} used, {formatBytes(db.freeBytes)} free.
        </p>
      )}
    </section>
  )
}

function BackupSection({ backupDir }: { backupDir?: string }) {
  const backup = useAdminBackup()
  const result = backup.data
  return (
    <section className="space-y-3">
      <div>
        <h3 className="flex items-center gap-2 font-medium">
          <DatabaseBackup className="h-4 w-4" aria-hidden="true" />
          Backup
        </h3>
        <p className="text-sm text-muted-foreground">
          A consistent copy of the database, without its free space, in{' '}
          {backupDir ? <span className="font-mono break-all">{backupDir}</span> : 'the backup directory'} (
          <code>AI_OBSERVER_BACKUP_DIR</code>). Needs free disk space of about the used size.
        </p>
      </div>
      <Button variant="outline" onClick={() => backup.mutate(undefined)} disabled={backup.isPending}>
        {backup.isPending ? 'Backing up…' : 'Back up now'}
      </Button>
      {backup.isError && <ErrorAlert error={backup.error} fallback="The backup failed." />}
      {result && (
        <div className="space-y-1 text-sm" role="status">
          <p>
            Backup written ({formatBytes(result.sizeBytes)} in {(result.durationMs / 1000).toFixed(1)} s):
          </p>
          <p className="flex min-w-0 items-center gap-1">
            <span className="font-mono break-all">{result.path}</span>
            <CopyButton value={result.path} label="Copy backup path" />
          </p>
        </div>
      )}
    </section>
  )
}

function ErrorAlert({ error, fallback }: { error: unknown; fallback: string }) {
  return (
    <Alert variant="destructive">
      <AlertDescription>{errorText(error, fallback)}</AlertDescription>
    </Alert>
  )
}

function describeCounts(c: Pick<AdminDeleteResponse, 'logs' | 'metrics' | 'spans' | 'traces'>): string {
  const parts = [
    `${formatCount(c.spans)} spans${c.traces ? ` (${formatCount(c.traces)} traces)` : ''}`,
    `${formatCount(c.logs)} logs`,
    `${formatCount(c.metrics)} metric points`,
  ]
  return parts.join(', ')
}
