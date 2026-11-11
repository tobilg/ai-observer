import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Link, MemoryRouter, Outlet, Route, Routes } from 'react-router-dom'
import { RouteErrorBoundary } from '../RouteErrorBoundary'
import { ErrorBoundary } from '../ErrorBoundary'

function Boom(): never {
  throw new Error('render failed')
}

function Shell() {
  return (
    <>
      <Link to="/ok">Go to ok</Link>
      <Link to="/boom">Go to boom</Link>
      <Outlet />
    </>
  )
}

// Every route renders its boundary at the same position, as App does, so
// React keeps one boundary instance across routes
function renderApp(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/" element={<Shell />}>
          <Route path="boom" element={<RouteErrorBoundary><Boom /></RouteErrorBoundary>} />
          <Route path="ok" element={<RouteErrorBoundary><p>Fine page</p></RouteErrorBoundary>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('RouteErrorBoundary', () => {
  it('clears a page error when another route is opened', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    renderApp('/boom')
    expect(screen.getByText('Something went wrong')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('link', { name: 'Go to ok' }))

    expect(screen.getByText('Fine page')).toBeInTheDocument()
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument()
    vi.restoreAllMocks()
  })
})

describe('ErrorBoundary', () => {
  it('resets when a reset key changes', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    let fail = true
    function Maybe() {
      if (fail) throw new Error('nope')
      return <p>recovered</p>
    }
    const { rerender } = render(<ErrorBoundary resetKeys={['a']}><Maybe /></ErrorBoundary>)
    expect(screen.getByText('nope')).toBeInTheDocument()

    fail = false
    rerender(<ErrorBoundary resetKeys={['a']}><Maybe /></ErrorBoundary>)
    expect(screen.getByText('nope')).toBeInTheDocument()

    rerender(<ErrorBoundary resetKeys={['b']}><Maybe /></ErrorBoundary>)
    expect(screen.getByText('recovered')).toBeInTheDocument()
    vi.restoreAllMocks()
  })
})
