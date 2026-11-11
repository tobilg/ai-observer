import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryError } from '../query-error'
import { httpError } from '@/lib/errors'

describe('QueryError', () => {
  it('shows the error message and retries', async () => {
    const onRetry = vi.fn()
    render(<QueryError error={httpError(400, 'invalid cursor')} title="Couldn't load logs" onRetry={onRetry} />)

    expect(screen.getByRole('alert')).toHaveTextContent("Couldn't load logs")
    expect(screen.getByText('invalid cursor')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalled()
  })

  it('shows the sign-in hint on 401 and the fallback otherwise', () => {
    const { rerender } = render(<QueryError error={httpError(401)} />)
    expect(screen.getByText(/\?token=/)).toBeInTheDocument()

    rerender(<QueryError error={new Error('x')} fallback="Failed to load traces" />)
    expect(screen.getByText('Failed to load traces')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
  })

  it('disables the retry while it runs', () => {
    render(<QueryError error={httpError(500)} onRetry={() => {}} retrying />)
    expect(screen.getByRole('button', { name: 'Retry' })).toBeDisabled()
  })
})
