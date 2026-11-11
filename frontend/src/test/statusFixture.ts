import type { StatusResponse } from '@/types/status'

type Sections = Pick<StatusResponse, 'server' | 'database' | 'telemetry' | 'settings'>

/**
 * A complete GET /api/status response for tests: a server in serve mode
 * with OTLP over HTTP and gRPC, a small database and default settings.
 * Top-level fields replace the defaults; the sections are merged, so a test
 * names only the fields it is about.
 */
export function statusFixture(
  overrides: Partial<Omit<StatusResponse, keyof Sections>> & { [K in keyof Sections]?: Partial<Sections[K]> } = {},
): StatusResponse {
  const { server, database, telemetry, settings, ...rest } = overrides
  return {
    version: 'v1',
    gitCommit: 'unknown',
    buildDate: 'unknown',
    mode: 'serve',
    startedAt: '2026-11-11T00:00:00Z',
    pricing: [],
    imports: [],
    ...rest,
    server: { host: '127.0.0.1', apiPort: 8080, otlpPort: 4318, otlpEnabled: true, otlpGrpcPort: 4317, otlpGrpcEnabled: true, ...server },
    database: {
      path: '/data/ai-observer.duckdb', sizeBytes: 1_048_576, walSizeBytes: 0,
      usedBytes: 1_048_576, freeBytes: 0, compactRecommended: false, ...database,
    },
    telemetry: { traceCount: 0, spanCount: 0, logCount: 0, metricCount: 0, serviceCount: 0, services: [], ...telemetry },
    settings: {
      retentionDays: 0, redaction: 'secrets', authEnabled: false, mcpEnabled: false, storeAllSpans: false,
      maxDbSizeBytes: 0, adminApiEnabled: false, tlsEnabled: false, logLevel: 'INFO', logFormat: 'text',
      corsOrigin: 'http://localhost:5173', skipMigrationBackup: false, ...settings,
    },
  }
}
