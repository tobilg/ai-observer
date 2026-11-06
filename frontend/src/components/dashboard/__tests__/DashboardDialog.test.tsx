import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DashboardDialog } from '../DashboardDialog'
import { fetchDashboardFromUrl } from '@/lib/dashboard-export'
import { WIDGET_TYPES } from '@/types/dashboard'

vi.mock('@/lib/dashboard-export', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/dashboard-export')>()),
  fetchDashboardFromUrl: vi.fn(),
}))

const validExport = {
  schemaVersion: 1,
  name: 'Imported Dashboard',
  description: 'From a teammate',
  widgets: [
    { widgetType: WIDGET_TYPES.STATS_TRACES, gridColumn: 1, gridRow: 1, colSpan: 1, rowSpan: 1 },
    { widgetType: WIDGET_TYPES.STATS_TRACES, gridColumn: 2, gridRow: 1, colSpan: 1, rowSpan: 1 },
  ],
}

function renderDialog(props: Partial<React.ComponentProps<typeof DashboardDialog>> = {}) {
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  const onImport = vi.fn().mockResolvedValue(undefined)
  const onOpenChange = vi.fn()
  render(
    <DashboardDialog
      open
      mode="create"
      onOpenChange={onOpenChange}
      onSubmit={onSubmit}
      onImport={onImport}
      {...props}
    />
  )
  return { onSubmit, onImport, onOpenChange }
}

function jsonFile(content: string, name = 'dashboard.json') {
  return new File([content], name, { type: 'application/json' })
}

describe('DashboardDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('creates a dashboard once it has a name', async () => {
    const user = userEvent.setup()
    const { onSubmit, onOpenChange } = renderDialog()

    const create = screen.getByRole('button', { name: 'Create' })
    expect(create).toBeDisabled()

    await user.type(screen.getByLabelText('Name'), '  Costs  ')
    await user.click(create)

    expect(onSubmit).toHaveBeenCalledWith('Costs', undefined)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('passes the trimmed description', async () => {
    const user = userEvent.setup()
    const { onSubmit } = renderDialog()

    await user.type(screen.getByLabelText('Name'), 'Costs')
    await user.type(screen.getByLabelText('Description (optional)'), ' Daily spend ')
    await user.click(screen.getByRole('button', { name: 'Create' }))

    expect(onSubmit).toHaveBeenCalledWith('Costs', 'Daily spend')
  })

  it('stays open when saving fails', async () => {
    const user = userEvent.setup()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const onSubmit = vi.fn().mockRejectedValue(new Error('offline'))
    const { onOpenChange } = renderDialog({ onSubmit })

    await user.type(screen.getByLabelText('Name'), 'Costs')
    await user.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() => expect(screen.getByRole('button', { name: 'Create' })).toBeEnabled())
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('renames a dashboard in edit mode', async () => {
    const user = userEvent.setup()
    const { onSubmit } = renderDialog({ mode: 'edit', initialName: 'Old name' })

    expect(screen.getByText('Rename Dashboard')).toBeInTheDocument()
    const input = await screen.findByDisplayValue('Old name')
    await user.clear(input)
    await user.type(input, 'New name')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(onSubmit).toHaveBeenCalledWith('New name', undefined)
  })

  it('imports a valid dashboard file after showing a preview', async () => {
    const user = userEvent.setup()
    const { onImport, onSubmit } = renderDialog()

    await user.click(screen.getByRole('tab', { name: 'Import' }))
    expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled()

    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    await user.upload(input, jsonFile(JSON.stringify(validExport)))

    expect(await screen.findByText('Imported Dashboard')).toBeInTheDocument()
    expect(screen.getByText('From a teammate')).toBeInTheDocument()
    expect(screen.getByText('2 widgets')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Import' }))
    expect(onImport).toHaveBeenCalledWith(validExport)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('rejects files that are not JSON or not a dashboard', async () => {
    const user = userEvent.setup()
    renderDialog()
    await user.click(screen.getByRole('tab', { name: 'Import' }))
    const input = document.querySelector('input[type="file"]') as HTMLInputElement

    await user.upload(input, jsonFile('{not json'))
    expect(await screen.findByText('Invalid JSON file')).toBeInTheDocument()

    await user.upload(input, jsonFile(JSON.stringify({ name: 'No widgets' })))
    await waitFor(() => expect(screen.queryByText('Invalid JSON file')).not.toBeInTheDocument())
    expect(screen.getByText('Invalid file:')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Import' })).toBeDisabled()
  })

  it('imports a dashboard from a URL and shows fetch errors', async () => {
    const user = userEvent.setup()
    vi.mocked(fetchDashboardFromUrl)
      .mockResolvedValueOnce({ error: 'Failed to fetch: 404 Not Found' })
      .mockResolvedValueOnce({ data: validExport })
    const { onImport } = renderDialog()

    await user.click(screen.getByRole('tab', { name: 'Import' }))
    await user.click(screen.getByRole('button', { name: /From URL/ }))
    const url = screen.getByLabelText('Dashboard URL')
    await user.type(url, 'https://example.com/d.json')

    await user.click(screen.getByRole('button', { name: 'Fetch' }))
    expect(await screen.findByText('Failed to fetch: 404 Not Found')).toBeInTheDocument()

    await user.type(url, '{Enter}')
    expect(await screen.findByText('Imported Dashboard')).toBeInTheDocument()
    expect(fetchDashboardFromUrl).toHaveBeenLastCalledWith('https://example.com/d.json')

    await user.click(screen.getByRole('button', { name: 'Import' }))
    expect(onImport).toHaveBeenCalledWith(validExport)
  })
})
