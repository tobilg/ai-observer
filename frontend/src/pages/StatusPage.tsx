import type { ReactNode } from 'react'
import { AlertTriangle, RefreshCw } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { CopyButton } from '@/components/common/copy-button'
import { EmptyState } from '@/components/common/empty-state'
import { PageHeader } from '@/components/common/page-header'
import { QueryError } from '@/components/common/query-error'
import { DatabaseCard } from '@/components/status/DatabaseCard'
import { MaintenanceCard } from '@/components/status/MaintenanceCard'
import { useLiveInvalidate } from '@/hooks/useLiveInvalidate'
import { useServiceNames } from '@/hooks/useServiceNames'
import { formatBytes, formatCount } from '@/lib/format'
import { queryKeys, useProviders, useStatus } from '@/lib/queries'
import {
  PRICING_MAX_AGE_DAYS,
  formatUptime,
  grpcOffReason,
  isPricingStale,
  modeLabel,
  pricingAgeDays,
  serverUrl,
  shortCommit,
} from '@/lib/status'
import { cn, formatTimestamp } from '@/lib/utils'
import { useTelemetryStore } from '@/stores/telemetryStore'
import type { StatusImport, StatusPricing, StatusResponse, StatusSettings } from '@/types/status'

// Names of the tools whose session files can be imported, until the
// providers have loaded
const TOOL_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  codex: 'OpenAI Codex',
  gemini: 'Gemini CLI',
}

export function StatusPage() {
  const status = useStatus()
  // Counts and sizes change with every ingestion: refresh on new telemetry
  // (at most every 10 seconds) and every 30 seconds while disconnected
  const updateCount = useTelemetryStore((s) => s.spansUpdateCount + s.logsUpdateCount + s.metricsUpdateCount)
  useLiveInvalidate({
    queryKey: queryKeys.status,
    enabled: true,
    intervalMs: 30_000,
    updateCount,
    debounceMs: 2_000,
    maxWaitMs: 10_000,
  })

  return (
    <div className="space-y-6">
      <PageHeader
        title="Status"
        description="The running AI Observer server: version, endpoints, database and settings"
        actions={
          status.data && (
            <Button variant="outline" onClick={() => status.refetch()} disabled={status.isFetching}>
              <RefreshCw className={cn('h-4 w-4 mr-2', status.isFetching && 'animate-spin')} aria-hidden="true" />
              Refresh
            </Button>
          )
        }
      />
      {status.isPending ? (
        <StatusSkeleton />
      ) : status.isError ? (
        <QueryError
          error={status.error}
          title="Couldn't load the server status"
          onRetry={() => status.refetch()}
          retrying={status.isFetching}
        />
      ) : (
        // Ages and uptime as of the response
        <StatusView status={status.data} now={status.dataUpdatedAt} />
      )}
    </div>
  )
}

function StatusView({ status, now }: { status: StatusResponse; now: number }) {
  const otlpUrl = serverUrl(status.server, status.server.otlpPort, window.location.hostname)
  const grpcUrl = serverUrl(status.server, status.server.otlpGrpcPort ?? 0, window.location.hostname)
  const apiUrl = serverUrl(status.server, status.server.apiPort, window.location.hostname)

  return (
    <div className="space-y-6">
      <div className="grid gap-6 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Server</CardTitle>
            <CardDescription>Version and how long it has been running</CardDescription>
          </CardHeader>
          <CardContent>
            <Fields>
              <Field label="Version">
                <span className="font-mono">{status.version}</span>
              </Field>
              <Field label="Commit">
                <span className="font-mono" title={status.gitCommit}>
                  {shortCommit(status.gitCommit)}
                </span>
              </Field>
              <Field label="Built">{formatBuildDate(status.buildDate)}</Field>
              <Field label="Mode">
                <Badge variant="secondary">{modeLabel(status.mode)}</Badge>
              </Field>
              {status.watchedTools && status.watchedTools.length > 0 && (
                <Field label="Watching">{status.watchedTools.map((tool) => TOOL_NAMES[tool] ?? tool).join(', ')}</Field>
              )}
              <Field label="Started">{formatTimestamp(status.startedAt)}</Field>
              <Field label="Uptime">{formatUptime(status.startedAt, now)}</Field>
            </Fields>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Endpoints</CardTitle>
            <CardDescription>Where the server listens ({status.server.host || 'all interfaces'})</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Fields>
              <Field label="OTLP">
                {status.server.otlpEnabled ? (
                  <span className="flex min-w-0 items-center gap-1">
                    <span className="font-mono break-all">{otlpUrl}</span>
                    <CopyButton value={otlpUrl} label="Copy OTLP endpoint" />
                  </span>
                ) : (
                  <span className="text-muted-foreground">Off (port {status.server.otlpPort} not in use)</span>
                )}
              </Field>
              <Field label="OTLP/gRPC">
                {status.server.otlpGrpcEnabled ? (
                  <span className="flex min-w-0 items-center gap-1">
                    <span className="font-mono break-all">{grpcUrl}</span>
                    <CopyButton value={grpcUrl} label="Copy OTLP/gRPC endpoint" />
                  </span>
                ) : (
                  <span className="text-muted-foreground">{grpcOffReason(status.server)}</span>
                )}
              </Field>
              <Field label="Dashboard and API">
                <span className="font-mono break-all">{apiUrl}</span>
              </Field>
              <Field label="MCP">
                {status.settings.mcpEnabled ? (
                  <span className="font-mono break-all">{apiUrl}/mcp</span>
                ) : (
                  <span className="text-muted-foreground">Off</span>
                )}
              </Field>
            </Fields>
            {!status.server.otlpEnabled && (
              <Alert>
                <AlertTriangle aria-hidden="true" />
                <AlertTitle>OTLP ingestion is off in watch mode</AlertTitle>
                <AlertDescription>
                  The server reads local session files only. Start it with <code>ai-observer serve</code> to receive
                  telemetry from AI tools over OTLP.
                </AlertDescription>
              </Alert>
            )}
          </CardContent>
        </Card>

        <DatabaseCard status={status} />

        <TelemetryCard status={status} />
      </div>

      <SettingsCard settings={status.settings} />
      <MaintenanceCard settings={status.settings} />
      <PricingCard pricing={status.pricing ?? []} now={now} />
      <ImportsCard imports={status.imports ?? []} />
    </div>
  )
}

function TelemetryCard({ status }: { status: StatusResponse }) {
  const { displayName } = useServiceNames()
  const t = status.telemetry
  return (
    <Card>
      <CardHeader>
        <CardTitle>Telemetry</CardTitle>
        <CardDescription>Everything stored, in all time ranges</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <Fields>
          <Field label="Traces">
            {formatCount(t.traceCount)} <span className="text-muted-foreground">({formatCount(t.spanCount)} spans)</span>
          </Field>
          <Field label="Logs">{formatCount(t.logCount)}</Field>
          <Field label="Metric points">{formatCount(t.metricCount)}</Field>
          <Field label="Oldest record">{t.oldest ? formatTimestamp(t.oldest) : '—'}</Field>
          <Field label="Newest record">{t.newest ? formatTimestamp(t.newest) : '—'}</Field>
          <Field label="Services">{formatCount(t.serviceCount)}</Field>
        </Fields>
        {(t.services ?? []).length > 0 && (
          <div className="flex flex-wrap gap-2">
            {t.services.map((service) => (
              <Badge key={service} variant="secondary" title={service}>
                {displayName(service)}
              </Badge>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function SettingsCard({ settings }: { settings: StatusSettings }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Settings</CardTitle>
        <CardDescription>What is stored and who may read it (environment variables of the server)</CardDescription>
      </CardHeader>
      <CardContent>
        <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Setting
            label="Retention"
            variable="AI_OBSERVER_RETENTION_DAYS"
            value={settings.retentionDays > 0 ? `${formatCount(settings.retentionDays)} days` : 'Keep all'}
          />
          <Setting
            label="Redaction"
            variable="AI_OBSERVER_REDACT"
            value={
              settings.redaction === 'all'
                ? 'Secrets and personal data'
                : settings.redaction === 'off'
                  ? 'Off'
                  : 'Secrets'
            }
            warn={settings.redaction === 'off'}
          />
          <Setting
            label="Authentication"
            variable="AI_OBSERVER_AUTH_TOKEN"
            value={settings.authEnabled ? 'Token required' : 'No token'}
            on={settings.authEnabled}
          />
          <Setting label="MCP endpoint" variable="AI_OBSERVER_MCP" value={settings.mcpEnabled ? 'Enabled' : 'Disabled'} on={settings.mcpEnabled} />
          <Setting
            label="Store all spans"
            variable="AI_OBSERVER_STORE_ALL_SPANS"
            value={settings.storeAllSpans ? 'On (noise spans kept)' : 'Off'}
            on={settings.storeAllSpans}
          />
          <Setting
            label="Size limit"
            variable="AI_OBSERVER_MAX_DB_SIZE"
            value={settings.maxDbSizeBytes ? formatBytes(settings.maxDbSizeBytes) : 'No limit'}
            on={Boolean(settings.maxDbSizeBytes)}
          />
          <Setting
            label="Admin API"
            variable="AI_OBSERVER_ADMIN_API"
            value={settings.adminApiEnabled ? 'Enabled' : 'Disabled'}
            on={Boolean(settings.adminApiEnabled)}
          />
          <Setting
            label="TLS"
            variable="AI_OBSERVER_TLS_CERT"
            value={settings.tlsEnabled ? 'HTTPS and gRPC over TLS' : 'Off'}
            on={Boolean(settings.tlsEnabled)}
          />
          {settings.logLevel && (
            <Setting
              label="Logs"
              variable="AI_OBSERVER_LOG_LEVEL"
              value={`${settings.logLevel}, ${settings.logFormat === 'json' ? 'JSON' : 'text'}`}
            />
          )}
          {settings.corsOrigin && (
            <Setting label="Dashboard origin (CORS)" variable="AI_OBSERVER_FRONTEND_URL" value={settings.corsOrigin} />
          )}
        </dl>
      </CardContent>
    </Card>
  )
}

function PricingCard({ pricing, now }: { pricing: StatusPricing[]; now: number }) {
  const stale = pricing.filter((p) => isPricingStale(p, now))
  return (
    <Card>
      <CardHeader>
        <CardTitle>Pricing</CardTitle>
        <CardDescription>The prices costs are calculated with, and overrides from AI_OBSERVER_PRICING_DIR</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {stale.length > 0 && (
          <Alert>
            <AlertTriangle aria-hidden="true" />
            <AlertTitle>
              Prices older than {PRICING_MAX_AGE_DAYS} days: {stale.map((p) => p.file).join(', ')}
            </AlertTitle>
            <AlertDescription>
              Costs of new models may be missing. Run <code>ai-observer pricing update</code> to fetch current prices.
            </AlertDescription>
          </Alert>
        )}
        {pricing.length === 0 ? (
          <p className="text-sm text-muted-foreground">No pricing files are loaded.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>File</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead className="text-right">Models</TableHead>
                <TableHead>Last updated</TableHead>
                <TableHead>Override</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {pricing.map((p) => {
                const age = pricingAgeDays(p, now)
                return (
                  <TableRow key={p.file}>
                    <TableCell className="font-mono">{p.file}</TableCell>
                    <TableCell>{p.provider || '—'}</TableCell>
                    <TableCell className="text-right tabular-nums">{formatCount(p.models)}</TableCell>
                    <TableCell>
                      <span className="flex flex-wrap items-center gap-2">
                        {formatDate(p.overrideLastUpdated || p.lastUpdated)}
                        {isPricingStale(p, now) ? (
                          <Badge variant="destructive">{age} days old</Badge>
                        ) : (
                          age !== null && <span className="text-xs text-muted-foreground">{age === 0 ? 'today' : `${age} day${age === 1 ? '' : 's'} ago`}</span>
                        )}
                      </span>
                    </TableCell>
                    <TableCell className="max-w-64">
                      {p.override ? (
                        <span className="block truncate font-mono text-xs" title={p.override}>
                          {p.override}
                        </span>
                      ) : (
                        <span className="text-muted-foreground">Built-in</span>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function ImportsCard({ imports }: { imports: StatusImport[] }) {
  const providers = useProviders()
  const toolName = (tool: string) =>
    providers.data?.providers?.find((p) => p.tool === tool)?.displayName ?? TOOL_NAMES[tool] ?? tool
  const imported = imports.filter((i) => i.files > 0)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Imported session files</CardTitle>
        <CardDescription>
          Files read by <code>ai-observer import</code> and <code>watch</code>, per tool
        </CardDescription>
      </CardHeader>
      <CardContent>
        {imported.length === 0 ? (
          <EmptyState
            className="py-6"
            title="No session files imported"
            description={
              <>
                Run <code>ai-observer import all</code> or <code>ai-observer watch all</code> to read local Claude Code, Codex
                and Gemini CLI sessions.
              </>
            }
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tool</TableHead>
                <TableHead className="text-right">Files</TableHead>
                <TableHead className="text-right">Records</TableHead>
                <TableHead>Last import</TableHead>
                <TableHead className="text-right">Partly read</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {imports.map((i) => (
                <TableRow key={i.tool}>
                  <TableCell>{toolName(i.tool)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(i.files)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(i.records)}</TableCell>
                  <TableCell>{i.lastImport ? formatTimestamp(i.lastImport) : '—'}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {i.partialFiles > 0 ? (
                      <Badge variant="outline" title="Files a ranged import read only partly; the next import reads them again">
                        {formatCount(i.partialFiles)}
                      </Badge>
                    ) : (
                      '0'
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

// ============================================================================
// Parts
// ============================================================================

function Fields({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-1 gap-x-4 gap-y-2 text-sm sm:grid-cols-[minmax(7rem,auto)_1fr]">{children}</dl>
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 mb-2 sm:mb-0">{children}</dd>
    </>
  )
}

interface SettingProps {
  label: string
  variable: string
  value: string
  // Highlights a setting that is turned on, or one that needs attention
  on?: boolean
  warn?: boolean
}

function Setting({ label, variable, value, on = false, warn = false }: SettingProps) {
  return (
    <div className="space-y-1">
      <dt className="text-sm font-medium">{label}</dt>
      <dd className="space-y-1">
        <Badge variant={warn ? 'destructive' : on ? 'default' : 'secondary'}>{value}</Badge>
        <p className="font-mono text-xs text-muted-foreground break-all">{variable}</p>
      </dd>
    </div>
  )
}

function StatusSkeleton() {
  return (
    <div role="status" aria-busy="true" aria-label="Loading status" className="grid gap-6 md:grid-cols-2">
      {Array.from({ length: 4 }, (_, i) => (
        <Skeleton key={i} className="h-56" />
      ))}
      <Skeleton className="h-32 md:col-span-2" />
    </div>
  )
}

// The build date ldflags set: RFC 3339, or "unknown" for development builds
function formatBuildDate(value: string): string {
  const date = new Date(value)
  return value && Number.isFinite(date.getTime()) ? formatTimestamp(date) : value || '—'
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isFinite(date.getTime())
    ? date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
    : value || '—'
}
