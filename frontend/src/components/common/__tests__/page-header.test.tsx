import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PageHeader } from '../page-header'

describe('PageHeader', () => {
  it('shows the title, description and actions and sets the tab title', () => {
    const { unmount } = render(
      <PageHeader title="Traces" description="View and analyze traces" actions={<button>3 new traces</button>} />,
    )
    expect(screen.getByRole('heading', { level: 1, name: 'Traces' })).toBeInTheDocument()
    expect(screen.getByText('View and analyze traces')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '3 new traces' })).toBeInTheDocument()
    expect(document.title).toBe('Traces · AI Observer')

    unmount()
    expect(document.title).toBe('AI Observer')
  })

  it('can name the tab differently', () => {
    render(<PageHeader title="Session" documentTitle="Session abc123" />)
    expect(document.title).toBe('Session abc123 · AI Observer')
  })
})
