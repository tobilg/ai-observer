import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { NativeSelect, NativeSelectOptGroup, NativeSelectOption } from '../native-select'

describe('NativeSelect', () => {
  const renderSelect = (props = {}) =>
    render(
      <NativeSelect data-testid="select" {...props}>
        <NativeSelectOption value="">Select an option</NativeSelectOption>
        <NativeSelectOption value="1">Option 1</NativeSelectOption>
        <NativeSelectOption value="2">Option 2</NativeSelectOption>
        <NativeSelectOption value="3">Option 3</NativeSelectOption>
      </NativeSelect>
    )

  it('renders a native select with base styles', () => {
    renderSelect()
    const select = screen.getByTestId('select')
    expect(select.tagName).toBe('SELECT')
    expect(select).toHaveClass('w-full', 'rounded-md', 'border', 'h-9')
  })

  it('applies a custom className to the select', () => {
    renderSelect({ className: 'h-10 custom-class' })
    const select = screen.getByTestId('select')
    expect(select).toHaveClass('custom-class', 'h-10')
    expect(select).not.toHaveClass('h-9')
  })

  it('forwards ref', () => {
    const ref = { current: null }
    render(
      <NativeSelect ref={ref}>
        <NativeSelectOption>Option</NativeSelectOption>
      </NativeSelect>
    )
    expect(ref.current).toBeInstanceOf(HTMLSelectElement)
  })

  it('renders options and option groups', () => {
    render(
      <NativeSelect aria-label="Range">
        <NativeSelectOptGroup label="Relative">
          <NativeSelectOption value="1h">Last 1 hour</NativeSelectOption>
        </NativeSelectOptGroup>
        <NativeSelectOptGroup label="Custom">
          <NativeSelectOption value="custom">Custom date range...</NativeSelectOption>
        </NativeSelectOptGroup>
      </NativeSelect>
    )
    expect(screen.getByRole('group', { name: 'Relative' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Custom date range...' })).toBeInTheDocument()
  })

  it('reports changes of a controlled value', async () => {
    const handleChange = vi.fn()
    const user = userEvent.setup()
    renderSelect({ value: '1', onChange: handleChange })

    await user.selectOptions(screen.getByTestId('select'), '2')

    expect(handleChange).toHaveBeenCalled()
  })

  it('works uncontrolled', async () => {
    const user = userEvent.setup()
    renderSelect({ defaultValue: '' })
    const select = screen.getByTestId('select')

    await user.selectOptions(select, '2')

    expect(select).toHaveValue('2')
  })

  it('cannot be changed when disabled', async () => {
    const handleChange = vi.fn()
    const user = userEvent.setup()
    renderSelect({ disabled: true, onChange: handleChange })
    const select = screen.getByTestId('select')

    expect(select).toBeDisabled()
    await user.selectOptions(select, '2')
    expect(handleChange).not.toHaveBeenCalled()
  })

  it('can be associated with a label', () => {
    render(
      <>
        <label htmlFor="test-select">Test Label</label>
        <NativeSelect id="test-select">
          <NativeSelectOption>Option</NativeSelectOption>
        </NativeSelect>
      </>
    )
    expect(screen.getByLabelText('Test Label')).toBeInTheDocument()
  })

  it('is a combobox', () => {
    renderSelect()
    expect(screen.getByRole('combobox')).toBe(screen.getByTestId('select'))
  })
})
