import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AppSidebar } from '../Sidebar'
import { SidebarProvider, useSidebar } from '@/components/ui/sidebar'
import { api } from '@/lib/api'
import { renderWithProviders } from '@/test/queryWrapper'
import type { StatusResponse } from '@/types/status'

vi.mock('@/lib/api', () => ({
  api: {
    getDashboards: vi.fn(),
    getStatus: vi.fn(),
  },
}))

function OpenMobileSidebar() {
  const { setOpenMobile } = useSidebar()
  return <button onClick={() => setOpenMobile(true)}>Open menu</button>
}

function renderSidebar(route: string) {
  return renderWithProviders(
    <SidebarProvider>
      <OpenMobileSidebar />
      <AppSidebar />
    </SidebarProvider>,
    { route }
  )
}

const originalMatchMedia = window.matchMedia

function setMobile(mobile: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: mobile,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

describe('AppSidebar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.getDashboards).mockResolvedValue({
      dashboards: [
        { id: 'd1', name: 'Default Dashboard', isDefault: true, createdAt: '', updatedAt: '' },
        { id: 'd2', name: 'Costs', isDefault: false, createdAt: '', updatedAt: '' },
      ],
    })
    vi.mocked(api.getStatus).mockResolvedValue({ version: 'v0.7.1', mode: 'serve' } as StatusResponse)
  })

  afterEach(() => {
    window.matchMedia = originalMatchMedia
  })

  it('highlights the current page, also below it, and marks it for assistive technology', async () => {
    renderSidebar('/traces/otel_trace/abc')

    const traces = screen.getByRole('link', { name: 'Traces' })
    expect(traces).toHaveAttribute('data-active', 'true')
    expect(traces).toHaveAttribute('aria-current', 'page')
    const logs = screen.getByRole('link', { name: 'Logs' })
    expect(logs).toHaveAttribute('data-active', 'false')
    expect(logs).not.toHaveAttribute('aria-current')

    // No dashboard is current on another page
    await screen.findByRole('link', { name: /Costs/ })
    expect(screen.getByRole('link', { name: /Default Dashboard/ })).toHaveAttribute('data-active', 'false')
  })

  it('highlights the shown dashboard', async () => {
    renderSidebar('/dashboard/d2')

    const costs = await screen.findByRole('link', { name: /Costs/ })
    expect(costs).toHaveAttribute('data-active', 'true')
    expect(costs).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: /Default Dashboard/ })).toHaveAttribute('data-active', 'false')
  })

  it('links the status page and shows the server version', async () => {
    renderSidebar('/status')

    expect(screen.getByRole('link', { name: 'Status' })).toHaveAttribute('data-active', 'true')
    expect(screen.getByRole('link', { name: 'Documentation' })).toHaveAttribute('href', '/docs')
    expect(await screen.findByRole('link', { name: 'AI Observer v0.7.1' })).toHaveAttribute('href', '/status')
  })

  it('closes on phones when a page is chosen', async () => {
    setMobile(true)
    const user = userEvent.setup()
    renderSidebar('/')

    await user.click(screen.getByRole('button', { name: 'Open menu' }))
    await user.click(await screen.findByRole('link', { name: 'Metrics' }))

    await waitFor(() => expect(screen.queryByRole('link', { name: 'Metrics' })).not.toBeInTheDocument())
  })
})
