// Pure helpers of the status page (GET /api/status)
import type { ServerMode, StatusDatabase, StatusPricing, StatusServer, StatusSettings } from '@/types/status'

const DAY_MS = 86_400_000

/**
 * Pricing data older than this is stale: the release check and the daily
 * "Pricing freshness" workflow allow 7 days (make pricing-update)
 */
export const PRICING_MAX_AGE_DAYS = 7

/** When the prices in use were last updated: the newer of the built-in file and its override */
export function pricingUpdatedAt(pricing: StatusPricing): Date | null {
  const times = [pricing.lastUpdated, pricing.overrideLastUpdated]
    .filter((t): t is string => Boolean(t))
    .map((t) => new Date(t).getTime())
    .filter((t) => Number.isFinite(t))
  return times.length > 0 ? new Date(Math.max(...times)) : null
}

/** Whole days since the prices were updated, or null when unknown */
export function pricingAgeDays(pricing: StatusPricing, now: number): number | null {
  const updated = pricingUpdatedAt(pricing)
  return updated ? Math.max(0, Math.floor((now - updated.getTime()) / DAY_MS)) : null
}

export function isPricingStale(pricing: StatusPricing, now: number): boolean {
  const age = pricingAgeDays(pricing, now)
  return age !== null && age > PRICING_MAX_AGE_DAYS
}

/** How long the server has been running: "45 seconds", "12 minutes", "3h 5m", "2d 4h" */
export function formatUptime(startedAt: string, now: number): string {
  const started = new Date(startedAt).getTime()
  if (!Number.isFinite(started)) {
    return '—'
  }
  const seconds = Math.max(0, Math.floor((now - started) / 1000))
  if (seconds < 60) return `${seconds} second${seconds === 1 ? '' : 's'}`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'}`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ${minutes % 60}m`
  return `${Math.floor(hours / 24)}d ${hours % 24}h`
}

/**
 * The address clients use for a port of the server: a wildcard listen
 * address (0.0.0.0, ::) is replaced by the host the dashboard was opened
 * with
 */
export function serverUrl(server: Pick<StatusServer, 'host'>, port: number, pageHost: string): string {
  const wildcard = server.host === '' || server.host === '0.0.0.0' || server.host === '::' || server.host === '[::]'
  let host = wildcard ? pageHost : server.host
  if (host.includes(':') && !host.startsWith('[')) {
    host = `[${host}]` // IPv6
  }
  return `http://${host}:${port}`
}

/** What a server mode does, for display */
export function modeLabel(mode: ServerMode): string {
  switch (mode) {
    case 'watch':
      return 'watch (session files)'
    case 'serve+watch':
      return 'serve + watch (OTLP and session files)'
    default:
      return 'serve (OTLP)'
  }
}

/**
 * Why OTLP/gRPC does not listen: in watch mode no OTLP server runs, and port
 * 0 turns it off (AI_OBSERVER_OTLP_GRPC_PORT=off)
 */
export function grpcOffReason(server: Pick<StatusServer, 'otlpEnabled' | 'otlpGrpcPort'>): string {
  if (!server.otlpEnabled) return 'Off (watch mode)'
  if (!server.otlpGrpcPort) return 'Off (AI_OBSERVER_OTLP_GRPC_PORT=off)'
  return 'Off'
}

/** A git commit for display: its first 12 characters ("unknown" as is) */
export function shortCommit(commit: string): string {
  return /^[0-9a-f]{13,}$/i.test(commit) ? commit.slice(0, 12) : commit
}

/**
 * Size from which a database that keeps all telemetry is pointed out (as by
 * the server's startup log)
 */
export const LARGE_DATABASE_BYTES = 1_073_741_824

/** Whether nothing bounds the stored telemetry: no retention and no size limit */
export function isUnbounded(settings: Pick<StatusSettings, 'retentionDays' | 'maxDbSizeBytes'>): boolean {
  return settings.retentionDays <= 0 && settings.maxDbSizeBytes <= 0
}

/** The share of the database file that is free blocks (0 to 1), or null when unknown */
export function freeShare(db: Pick<StatusDatabase, 'usedBytes' | 'freeBytes'>): number | null {
  const total = db.usedBytes + db.freeBytes
  return total > 0 ? db.freeBytes / total : null
}

/**
 * Days until the database reaches limitBytes at its growth per day, or null
 * when it does not grow or the limit is reached
 */
export function daysUntil(db: Pick<StatusDatabase, 'usedBytes' | 'growthBytesPerDay'>, limitBytes: number): number | null {
  const growth = db.growthBytesPerDay ?? 0
  if (growth <= 0 || db.usedBytes >= limitBytes) return null
  return Math.floor((limitBytes - db.usedBytes) / growth)
}

/**
 * The time range of whole local days, as the delete command reads --from
 * and --to: from the start of the first day to the end of the last, in
 * RFC 3339. Days are YYYY-MM-DD (an <input type="date">); null when one is
 * missing or invalid, or from is after to.
 */
export function localDayRange(fromDay: string, toDay: string): { from: string; to: string } | null {
  const parse = (day: string): [number, number, number] | null => {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day)
    return m ? [Number(m[1]), Number(m[2]) - 1, Number(m[3])] : null
  }
  const a = parse(fromDay)
  const b = parse(toDay)
  if (!a || !b) return null
  const from = new Date(a[0], a[1], a[2], 0, 0, 0, 0)
  const to = new Date(b[0], b[1], b[2], 23, 59, 59, 999)
  if (!Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime()) || from > to) return null
  return { from: from.toISOString(), to: to.toISOString() }
}
