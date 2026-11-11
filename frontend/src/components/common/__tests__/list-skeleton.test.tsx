import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ListSkeleton, Refreshing } from '../list-skeleton'

describe('ListSkeleton', () => {
  it('shows busy placeholder rows', () => {
    const { container } = render(<ListSkeleton rows={3} label="Loading traces" />)
    expect(screen.getByRole('status', { name: 'Loading traces' })).toHaveAttribute('aria-busy', 'true')
    expect(container.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(3)
  })
})

describe('Refreshing', () => {
  it('dims and marks shown data while it reloads', () => {
    const { rerender } = render(<Refreshing active={false}><p>rows</p></Refreshing>)
    const wrapper = screen.getByText('rows').parentElement!
    expect(wrapper).toHaveAttribute('aria-busy', 'false')
    expect(wrapper).not.toHaveClass('opacity-60')

    rerender(<Refreshing active><p>rows</p></Refreshing>)
    expect(wrapper).toHaveAttribute('aria-busy', 'true')
    expect(wrapper).toHaveClass('opacity-60')
  })
})
