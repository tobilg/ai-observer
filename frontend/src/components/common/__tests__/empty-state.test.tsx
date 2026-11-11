import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { EmptyState } from '../empty-state'

describe('EmptyState', () => {
  it('shows a message, a description and an action', () => {
    render(
      <EmptyState title="No traces in this time range" description="Widen the time range" action={<button>Clear filters</button>} />,
    )
    expect(screen.getByText('No traces in this time range')).toBeInTheDocument()
    expect(screen.getByText('Widen the time range')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeInTheDocument()
  })
})
