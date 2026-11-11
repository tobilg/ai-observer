import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { StatusPage } from '../StatusPage'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { renderWithProviders } from '@/test/queryWrapper'
import { statusFixture } from '@/test/statusFixture'
import type { StatusResponse } from '@/types/status'

vi.mock('@/lib/api', () => ({
  api: {
    getStatus: vi.fn(),
    getProviders: vi.fn(),
  },
}))

const day = 86_400_000
const isoDaysAgo = (days: number) => new Date(Date.now() - days * day).toISOString()

function status(overrides: Partial<StatusResponse> = {}): StatusResponse {
  return {
    version: 'v0.7.1',
    gitCommit: '0123456789abcdef0123456789abcdef01234567',
    buildDate: '2026-11-10T08:00:00Z',
    mode: 'serve',
    startedAt: new Date(Date.now() - 2 * 3_600_000 - 5 * 60_000).toISOString(),
    server: { host: '127.0.0.1', apiPort: 8080, otlpPort: 4318, otlpEnabled: true, otlpGrpcPort: 4317, otlpGrpcEnabled: true },
    database: { ...statusFixture().database, sizeBytes: 1_572_864, usedBytes: 1_310_720, freeBytes: 262_144, memoryLimit: '2GB', threads: 4 },
    telemetry: {
      traceCount: 1234, spanCount: 56789, logCount: 1_000_000, metricCount: 42, serviceCount: 2,
      services: ['claude-code', 'gemini_cli'], oldest: '2026-01-01T00:00:00Z', newest: '2026-11-11T00:00:00Z',
    },
    settings: {
      ...statusFixture().settings,
      retentionDays: 30, authEnabled: true, tlsEnabled: true, logLevel: 'DEBUG', logFormat: 'json', corsOrigin: 'https://observer.example.com',
    },
    pricing: [
      { file: 'claude.json', provider: 'anthropic', lastUpdated: isoDaysAgo(2), models: 20 },
      { file: 'gemini.json', provider: 'google', lastUpdated: isoDaysAgo(12), models: 8 },
      { file: 'codex.json', provider: 'openai', lastUpdated: isoDaysAgo(30), models: 9, override: '/prices/codex.json', overrideLastUpdated: isoDaysAgo(1) },
    ],
    imports: [
      { tool: 'claude-code', files: 12, records: 3456, lastImport: '2026-11-10T10:00:00Z', partialFiles: 1 },
      { tool: 'codex', files: 0, records: 0, partialFiles: 0 },
    ],
    ...overrides,
  }
}

describe('StatusPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getProviders).mockResolvedValue({ providers: [] })
  })

  it('shows version, endpoints, database, telemetry, settings and imports', async () => {
    const user = userEvent.setup()
    // After setup, which puts its own clipboard in place
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    vi.mocked(api.getStatus).mockResolvedValue(status())
    renderWithProviders(<StatusPage />)

    expect(screen.getByRole('heading', { level: 1, name: 'Status' })).toBeInTheDocument()
    expect(document.title).toBe('Status · AI Observer')
    expect(screen.getByRole('status', { name: 'Loading status' })).toBeInTheDocument()

    expect(await screen.findByText('v0.7.1')).toBeInTheDocument()
    expect(screen.getByText('0123456789ab')).toBeInTheDocument()
    expect(screen.getByText('serve (OTLP)')).toBeInTheDocument()
    expect(screen.getByText('2h 5m')).toBeInTheDocument()
    expect(screen.getByText('http://127.0.0.1:4318')).toBeInTheDocument()
    expect(screen.getByText('1.5 MB')).toBeInTheDocument()
    expect(screen.getByText('2GB')).toBeInTheDocument()
    expect(screen.getByText('1,000,000')).toBeInTheDocument()
    expect(screen.getByText('(56,789 spans)')).toBeInTheDocument()
    expect(screen.getByText('Gemini CLI')).toBeInTheDocument()

    // Settings as labelled badges
    expect(screen.getByText('30 days')).toBeInTheDocument()
    expect(screen.getByText('Token required')).toBeInTheDocument()
    expect(screen.getByText('AI_OBSERVER_REDACT')).toBeInTheDocument()
    expect(screen.getByText('HTTPS and gRPC over TLS')).toBeInTheDocument()
    expect(screen.getByText('DEBUG, JSON')).toBeInTheDocument()
    expect(screen.getByText('https://observer.example.com')).toBeInTheDocument()

    // Imports per tool
    const claudeRow = screen.getByRole('row', { name: /Claude Code/ })
    expect(within(claudeRow).getByText('3,456')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Copy database path' }))
    expect(writeText).toHaveBeenCalledWith('/data/ai-observer.duckdb')
    await user.click(screen.getByRole('button', { name: 'Copy OTLP endpoint' }))
    expect(writeText).toHaveBeenCalledWith('http://127.0.0.1:4318')
    await user.click(screen.getByRole('button', { name: 'Copy OTLP/gRPC endpoint' }))
    expect(writeText).toHaveBeenCalledWith('http://127.0.0.1:4317')
  })

  it('shows serve --watch with the watched tools', async () => {
    vi.mocked(api.getStatus).mockResolvedValue(status({ mode: 'serve+watch', watchedTools: ['claude-code', 'codex'] }))
    renderWithProviders(<StatusPage />)

    expect(await screen.findByText('serve + watch (OTLP and session files)')).toBeInTheDocument()
    expect(screen.getByText('Claude Code, OpenAI Codex')).toBeInTheDocument()
    expect(screen.getByText('http://127.0.0.1:4318')).toBeInTheDocument()
    expect(screen.queryByText('OTLP ingestion is off in watch mode')).not.toBeInTheDocument()
  })

  it('warns about prices older than 7 days, counting overrides', async () => {
    vi.mocked(api.getStatus).mockResolvedValue(status())
    renderWithProviders(<StatusPage />)

    expect(await screen.findByText('Prices older than 7 days: gemini.json')).toBeInTheDocument()
    expect(screen.getByText('12 days old')).toBeInTheDocument()
    // codex.json has an old built-in file but a fresh override
    expect(screen.getByText('/prices/codex.json')).toBeInTheDocument()
    expect(screen.queryByText('30 days old')).not.toBeInTheDocument()
  })

  it('tells that OTLP is off in watch mode', async () => {
    vi.mocked(api.getStatus).mockResolvedValue(
      status({ mode: 'watch', server: { ...statusFixture().server, otlpEnabled: false, otlpGrpcEnabled: false }, imports: [] })
    )
    renderWithProviders(<StatusPage />)

    expect(await screen.findByText('OTLP ingestion is off in watch mode')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Copy OTLP endpoint' })).not.toBeInTheDocument()
    expect(screen.getByText('Off (watch mode)')).toBeInTheDocument()
    expect(screen.getByText('No session files imported')).toBeInTheDocument()
  })

  it('shows the error with a retry', async () => {
    const user = userEvent.setup()
    vi.mocked(api.getStatus).mockRejectedValueOnce(httpError(401)).mockResolvedValue(status())
    renderWithProviders(<StatusPage />)

    expect(await screen.findByText("Couldn't load the server status")).toBeInTheDocument()
    expect(screen.getByText(/Authentication required/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('v0.7.1')).toBeInTheDocument()
  })
})
