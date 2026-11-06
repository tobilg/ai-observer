import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EditableText } from '../editable-text'

describe('EditableText', () => {
  it('follows the external value while not focused', () => {
    const { rerender } = render(<EditableText value="First" onSave={vi.fn()} isEditing />)
    expect(screen.getByRole('textbox')).toHaveValue('First')

    rerender(<EditableText value="Renamed elsewhere" onSave={vi.fn()} isEditing />)
    expect(screen.getByRole('textbox')).toHaveValue('Renamed elsewhere')
  })

  it('saves the trimmed edit on Enter', async () => {
    const onSave = vi.fn()
    render(<EditableText value="Old" onSave={onSave} isEditing />)

    const input = screen.getByRole('textbox')
    await userEvent.clear(input)
    await userEvent.type(input, '  New name  {Enter}')

    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onSave).toHaveBeenCalledWith('New name')
  })

  it('saves when focus leaves the input', async () => {
    const onSave = vi.fn()
    render(<EditableText value="Old" onSave={onSave} isEditing />)

    await userEvent.type(screen.getByRole('textbox'), '!')
    await userEvent.tab()

    expect(onSave).toHaveBeenCalledWith('Old!')
  })

  it('restores the value on Escape without saving', async () => {
    const onSave = vi.fn()
    render(<EditableText value="Old" onSave={onSave} isEditing />)

    const input = screen.getByRole('textbox')
    await userEvent.type(input, ' changed{Escape}')

    expect(input).toHaveValue('Old')
    expect(onSave).not.toHaveBeenCalled()
  })
})
