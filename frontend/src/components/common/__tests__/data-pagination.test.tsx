import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DataPagination } from '../data-pagination'

function setup(props: Partial<Parameters<typeof DataPagination>[0]> = {}) {
  const handlers = { onFirst: vi.fn(), onPrevious: vi.fn(), onNext: vi.fn(), onPageSizeChange: vi.fn() }
  render(<DataPagination page={2} pageSize={25} itemCount={25} total={12_345} hasNext {...handlers} {...props} />)
  return handlers
}

describe('DataPagination', () => {
  it('renders buttons, not links, and calls the handlers', async () => {
    const user = userEvent.setup()
    const handlers = setup()
    expect(screen.queryAllByRole('link')).toHaveLength(0)

    await user.click(screen.getByRole('button', { name: 'Go to first page' }))
    await user.click(screen.getByRole('button', { name: 'Go to previous page' }))
    await user.click(screen.getByRole('button', { name: 'Go to next page' }))
    expect(handlers.onFirst).toHaveBeenCalledTimes(1)
    expect(handlers.onPrevious).toHaveBeenCalledTimes(1)
    expect(handlers.onNext).toHaveBeenCalledTimes(1)
  })

  it('formats the counts and announces the range politely', () => {
    setup()
    const range = screen.getByText('26–50 of 12,345')
    expect(range).toHaveAttribute('aria-live', 'polite')
    expect(screen.getByText('Page 2 of 494')).toBeInTheDocument()
  })

  it('disables what is not possible', () => {
    setup({ page: 1, hasNext: false })
    expect(screen.getByRole('button', { name: 'Go to first page' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Go to previous page' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()
  })

  it('labels the page size select', async () => {
    const user = userEvent.setup()
    const handlers = setup()
    await user.selectOptions(screen.getByRole('combobox', { name: 'Rows per page' }), '50')
    expect(handlers.onPageSizeChange).toHaveBeenCalledWith(50)
  })

  it('renders nothing without rows', () => {
    const { container } = render(
      <DataPagination page={1} pageSize={10} itemCount={0} total={0} hasNext={false} onFirst={vi.fn()} onPrevious={vi.fn()} onNext={vi.fn()} onPageSizeChange={vi.fn()} />,
    )
    expect(container).toBeEmptyDOMElement()
  })
})
