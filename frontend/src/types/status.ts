// GET /api/status: the running server, its database and settings, and the
// admin API (/api/admin/*, AI_OBSERVER_ADMIN_API=true). Generated from the
// Go types (api.gen.ts).
export type {
  ServerMode,
  RedactionMode,
  StatusResponse,
  StatusServer,
  StatusDatabase,
  StatusTelemetry,
  StatusSettings,
  StatusPricing,
  StatusImport,
  DeleteScope,
  AdminDeleteRequest,
  AdminDatabaseSize,
  AdminDeleteResponse,
  AdminCheckpointResponse,
  AdminBackupRequest,
  AdminBackupResponse,
  AdminExportRequest,
  AdminExportResponse,
} from './api.gen'
