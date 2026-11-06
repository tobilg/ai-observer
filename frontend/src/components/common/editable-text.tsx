import { useState, useRef, useCallback } from 'react'
import { cn } from '@/lib/utils'

interface EditableTextProps {
  value: string
  onSave: (value: string) => void
  isEditing: boolean
  placeholder?: string
  className?: string
  inputClassName?: string
}

export function EditableText({
  value,
  onSave,
  isEditing,
  placeholder = '',
  className = '',
  inputClassName = '',
}: EditableTextProps) {
  const [editValue, setEditValue] = useState(value)
  const [isFocused, setIsFocused] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  // Set when Enter or Escape ended the edit, so the blur that follows does not
  // save again (Enter) or save the discarded edit (Escape)
  const endedByKeyRef = useRef(false)

  // The input shows the external value until it is focused; edits start from
  // the value at that moment
  const shownValue = isFocused ? editValue : value

  const handleSave = useCallback(() => {
    const trimmed = editValue.trim()
    if (trimmed !== value) {
      onSave(trimmed)
    }
    setIsFocused(false)
  }, [editValue, value, onSave])

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      handleSave()
      endedByKeyRef.current = true
      inputRef.current?.blur()
    } else if (e.key === 'Escape') {
      setEditValue(value)
      setIsFocused(false)
      endedByKeyRef.current = true
      inputRef.current?.blur()
    }
  }

  const handleFocus = () => {
    endedByKeyRef.current = false
    setEditValue(value)
    setIsFocused(true)
  }

  const handleBlur = () => {
    if (endedByKeyRef.current) {
      endedByKeyRef.current = false
      return
    }
    handleSave()
  }

  // When in edit mode, show an editable input with subtle styling
  if (isEditing) {
    return (
      <input
        ref={inputRef}
        type="text"
        value={shownValue}
        onChange={(e) => setEditValue(e.target.value)}
        onKeyDown={handleKeyDown}
        onFocus={handleFocus}
        onBlur={handleBlur}
        placeholder={placeholder}
        className={cn(
          // Base text styling from className
          className,
          // Input-specific styling
          'bg-transparent border-0 border-b-2 border-transparent rounded-none px-0 py-0.5 w-full',
          'hover:border-muted-foreground/30',
          'focus:border-primary focus:outline-none focus:ring-0',
          'placeholder:text-muted-foreground/50',
          'transition-colors',
          inputClassName
        )}
      />
    )
  }

  // When not in edit mode, show static text
  return (
    <span className={cn(className, !value && 'text-muted-foreground/50')}>
      {value || placeholder}
    </span>
  )
}
