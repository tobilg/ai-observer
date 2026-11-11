import type { ReactNode } from 'react'
import { AlertTriangle, Info } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { CopyButton } from '@/components/common/copy-button'
import { formatBytes, formatCount } from '@/lib/format'
import { LARGE_DATABASE_BYTES, daysUntil, freeShare, isUnbounded } from '@/lib/status'
import type { StatusResponse } from '@/types/status'

/**
 * The database file: its size and the free space inside it, how fast it
 * grows, and hints when nothing bounds its growth or compaction would
 * return much space
 */
export function DatabaseCard({ status }: { status: StatusResponse }) {
  const db = status.database
  const settings = status.settings
  const share = freeShare(db)
  const growth = db.growthBytesPerDay ?? 0
  const limit = settings.maxDbSizeBytes ?? 0
  const untilLimit = limit > 0 ? daysUntil(db, limit) : null
  const unbounded = isUnbounded(settings)
  const total = db.sizeBytes + db.walSizeBytes

  return (
    <Card>
      <CardHeader>
        <CardTitle>Database</CardTitle>
        <CardDescription>The DuckDB file all telemetry is stored in</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <dl className="grid grid-cols-1 gap-x-4 gap-y-2 text-sm sm:grid-cols-[minmax(7rem,auto)_1fr]">
          <Field label="Path">
            <span className="flex min-w-0 items-center gap-1">
              <span className="font-mono break-all">{db.path}</span>
              <CopyButton value={db.path} label="Copy database path" />
            </span>
          </Field>
          <Field label="Size">{formatBytes(db.sizeBytes)}</Field>
          {db.usedBytes !== undefined && (
            <Field label="Data">
              {formatBytes(db.usedBytes)}
              {share !== null && (
                <span className="text-muted-foreground">
                  {' '}
                  ({formatBytes(db.freeBytes ?? 0)} free inside the file, {Math.round(share * 100)}%)
                </span>
              )}
            </Field>
          )}
          <Field label="Write-ahead log">{db.walSizeBytes > 0 ? formatBytes(db.walSizeBytes) : 'None'}</Field>
          <Field label="Growth">
            {growth > 0 ? (
              <>
                about {formatBytes(growth)} a day
                {untilLimit !== null && (
                  <span className="text-muted-foreground"> (size limit in about {formatCount(untilLimit)} days)</span>
                )}
              </>
            ) : (
              <span className="text-muted-foreground">Known after a day of telemetry</span>
            )}
          </Field>
          <Field label="Memory limit">{db.memoryLimit || "DuckDB's default (80% of memory)"}</Field>
          <Field label="Threads">{db.threads ? formatCount(db.threads) : 'One per CPU core'}</Field>
        </dl>

        {unbounded && (
          <Alert>
            {total >= LARGE_DATABASE_BYTES ? <AlertTriangle aria-hidden="true" /> : <Info aria-hidden="true" />}
            <AlertTitle>All telemetry is kept</AlertTitle>
            <AlertDescription>
              <p>
                Nothing bounds the database{growth > 0 ? `, which grows by about ${formatBytes(growth)} a day` : ''}. Set{' '}
                <code>AI_OBSERVER_RETENTION_DAYS</code> to delete telemetry older than a number of days, or{' '}
                <code>AI_OBSERVER_MAX_DB_SIZE</code> (such as <code>20GB</code>) to delete the oldest days while the data
                exceeds a size.
              </p>
            </AlertDescription>
          </Alert>
        )}
        {db.compactRecommended && (
          <Alert>
            <Info aria-hidden="true" />
            <AlertTitle>{formatBytes(db.freeBytes ?? 0)} of the file are free</AlertTitle>
            <AlertDescription>
              <p>
                Deleted data leaves free blocks, which new data reuses, but the file keeps its size. To return the space to
                the disk, stop the server and run <code>ai-observer compact</code>.
              </p>
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 mb-2 sm:mb-0">{children}</dd>
    </>
  )
}
