import { afterEach, describe, expect, it } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import { AuthBanner } from '../AuthBanner'
import { useAuthStore } from '@/stores/authStore'

describe('AuthBanner', () => {
  afterEach(() => useAuthStore.setState({ authRequired: false }))

  it('shows the token hint while requests fail with 401', () => {
    render(<AuthBanner />)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    act(() => useAuthStore.getState().setAuthRequired(true))
    expect(screen.getByRole('alert')).toHaveTextContent('?token=<AI_OBSERVER_AUTH_TOKEN>')

    act(() => useAuthStore.getState().setAuthRequired(false))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
