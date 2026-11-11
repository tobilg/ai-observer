import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MaintenanceCard } from '../MaintenanceCard'
import { api } from '@/lib/api'
import { httpError } from '@/lib/errors'
import { renderWithProviders } from '@/test/queryWrapper'
import { statusFixture } from '@/test/statusFixture'
import type { StatusSettings } from '@/types/status'

vi.mock('@/lib/api', () => ({
  api: {
    getProviders: vi.fn(),
    adminDelete: vi.fn(),
    adminCheckpoint: vi.fn(),
    adminBackup: vi.fn(),
  },
}))

const settings = (overrides: Partial<StatusSettings> = {}): StatusSettings =>
  statusFixture({ settings: { adminApiEnabled: true, backupDir: '/data/backups', ...overrides } }).settings

const size = { sizeBytes: 750 * 1024 ** 2, walSizeBytes: 0, usedBytes: 500 * 1024 ** 2, freeBytes: 250 * 1024 ** 2, compactRecommended: true }

describe('MaintenanceCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getProviders).mockResolvedValue({
      providers: [{ tool: 'codex', serviceName: 'codex_cli_rs', displayName: 'OpenAI Codex', sessionFiles: true }],
    })
  })

  it('says how to enable the admin API when it is off', () => {
    renderWithProviders(<MaintenanceCard settings={settings({ adminApiEnabled: false })} />)
    expect(screen.getByText(/The admin API is off/)).toBeInTheDocument()
    expect(screen.getByText('AI_OBSERVER_ADMIN_API=true')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Count records/ })).not.toBeInTheDocument()
  })

  it('counts, confirms and deletes a range of days', async () => {
    const user = userEvent.setup()
    vi.mocked(api.adminDelete)
      .mockResolvedValueOnce({ dryRun: true, from: '', to: '', scope: 'all', logs: 5, metrics: 3, spans: 10, traces: 2 })
      .mockResolvedValueOnce({ dryRun: false, from: '', to: '', scope: 'all', logs: 5, metrics: 3, spans: 10, database: size })
    renderWithProviders(<MaintenanceCard settings={settings()} />)

    const deleteButton = screen.getByRole('button', { name: 'Delete…' })
    expect(deleteButton).toBeDisabled()
    expect(screen.getByRole('button', { name: /Count records/ })).toBeDisabled()

    await user.type(screen.getByLabelText('From'), '2026-11-01')
    await user.type(screen.getByLabelText('To'), '2026-11-03')
    await screen.findByRole('option', { name: 'OpenAI Codex' })
    await user.selectOptions(screen.getByLabelText('Tool'), 'codex')
    await user.click(screen.getByRole('button', { name: /Count records/ }))

    expect(await screen.findByText('Would delete 10 spans (2 traces), 5 logs, 3 metric points.')).toBeInTheDocument()
    const preview = vi.mocked(api.adminDelete).mock.calls[0][0]
    expect(preview).toMatchObject({ service: 'codex', scope: 'all', dryRun: true })
    expect(new Date(preview.from).getDate()).toBe(1)
    expect(new Date(preview.to).getDate()).toBe(3)

    await user.click(screen.getByRole('button', { name: 'Delete…' }))
    const dialog = await screen.findByRole('alertdialog')
    expect(within(dialog).getByText(/This deletes 10 spans \(2 traces\), 5 logs, 3 metric points from 2026-11-01 to 2026-11-03 of OpenAI Codex/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    expect(await screen.findByText(/Deleted 10 spans, 5 logs, 3 metric points\./)).toBeInTheDocument()
    expect(screen.getByText(/250.0 MB of it are now free blocks/)).toBeInTheDocument()
    const deletion = vi.mocked(api.adminDelete).mock.calls[1][0]
    expect(deletion.dryRun).toBeUndefined()
    expect(deletion).toMatchObject({ service: 'codex', scope: 'all', from: preview.from, to: preview.to })
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('asks for a new count when the selection changes', async () => {
    const user = userEvent.setup()
    vi.mocked(api.adminDelete).mockResolvedValue({ dryRun: true, from: '', to: '', scope: 'all', logs: 5, metrics: 0, spans: 0 })
    renderWithProviders(<MaintenanceCard settings={settings()} />)
    await user.type(screen.getByLabelText('From'), '2026-11-01')
    await user.type(screen.getByLabelText('To'), '2026-11-01')
    await user.click(screen.getByRole('button', { name: /Count records/ }))
    await screen.findByText(/Would delete/)
    expect(screen.getByRole('button', { name: 'Delete…' })).toBeEnabled()

    await user.selectOptions(screen.getByLabelText('What'), 'logs')
    expect(screen.queryByText(/Would delete/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete…' })).toBeDisabled()
  })

  it('cancels the deletion in the dialog', async () => {
    const user = userEvent.setup()
    vi.mocked(api.adminDelete).mockResolvedValue({ dryRun: true, from: '', to: '', scope: 'all', logs: 1, metrics: 0, spans: 0 })
    renderWithProviders(<MaintenanceCard settings={settings()} />)
    await user.type(screen.getByLabelText('From'), '2026-11-01')
    await user.type(screen.getByLabelText('To'), '2026-11-02')
    await user.click(screen.getByRole('button', { name: /Count records/ }))
    await screen.findByText(/Would delete/)
    await user.click(screen.getByRole('button', { name: 'Delete…' }))
    await user.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Cancel' }))
    expect(api.adminDelete).toHaveBeenCalledTimes(1)
  })

  it('shows the server message when counting fails', async () => {
    const user = userEvent.setup()
    vi.mocked(api.adminDelete).mockRejectedValue(httpError(409, 'another maintenance operation is running'))
    renderWithProviders(<MaintenanceCard settings={settings()} />)
    await user.type(screen.getByLabelText('From'), '2026-11-01')
    await user.type(screen.getByLabelText('To'), '2026-11-02')
    await user.click(screen.getByRole('button', { name: /Count records/ }))
    expect(await screen.findByText(/another maintenance operation is running/)).toBeInTheDocument()
  })

  it('runs a checkpoint and writes a backup', async () => {
    const user = userEvent.setup()
    vi.mocked(api.adminCheckpoint).mockResolvedValue({ database: size, durationMs: 120 })
    vi.mocked(api.adminBackup).mockResolvedValue({
      path: '/data/backups/ai-observer-backup-20261111-120000.duckdb', sizeBytes: 500 * 1024 ** 2,
      tables: { otel_logs: 10 }, schemaVersion: 11, durationMs: 4100,
    })
    renderWithProviders(<MaintenanceCard settings={settings()} />)
    expect(screen.getByText('/data/backups')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Run checkpoint' }))
    expect(await screen.findByText('Done: 750.0 MB file, 500.0 MB used, 250.0 MB free.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Back up now' }))
    expect(await screen.findByText('/data/backups/ai-observer-backup-20261111-120000.duckdb')).toBeInTheDocument()
    expect(screen.getByText(/500.0 MB in 4.1 s/)).toBeInTheDocument()
  })
})
