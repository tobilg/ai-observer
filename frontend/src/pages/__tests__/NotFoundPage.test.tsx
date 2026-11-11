import { describe, expect, it } from 'vitest'
import { screen } from '@testing-library/react'
import { NotFoundPage } from '../NotFoundPage'
import { renderWithProviders } from '@/test/queryWrapper'

describe('NotFoundPage', () => {
  it('names the address and links home', () => {
    renderWithProviders(<NotFoundPage />, { route: '/nope/here' })
    expect(screen.getByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
    expect(screen.getByText('/nope/here')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Go to the dashboard' })).toHaveAttribute('href', '/')
    expect(document.title).toBe('Page not found · AI Observer')
  })
})
