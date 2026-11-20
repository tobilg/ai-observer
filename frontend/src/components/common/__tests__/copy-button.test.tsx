import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { CopyButton } from '../copy-button'

describe('CopyButton', () => {
  const writeText = vi.fn()

  beforeEach(() => {
    vi.useFakeTimers()
    writeText.mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('copies the value and confirms for a moment', async () => {
    const onRowClick = vi.fn()
    render(
      <div onClick={onRowClick}>
        <CopyButton value="trace-123" label="Copy trace ID" />
      </div>,
    )

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy trace ID' }))
    })
    expect(writeText).toHaveBeenCalledWith('trace-123')
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(onRowClick).not.toHaveBeenCalled()

    act(() => vi.advanceTimersByTime(1500))
    expect(screen.getByRole('button', { name: 'Copy trace ID' })).toBeInTheDocument()
  })

  it('shows manual copy instructions when the host denies clipboard access', async () => {
    writeText.mockRejectedValue(new Error('denied'))
    render(<CopyButton value="x" />)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    })
    expect(screen.getByRole('button', { name: 'Copy' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Select and copy the text manually')
  })
})
