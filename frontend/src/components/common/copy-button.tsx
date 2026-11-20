import { useEffect, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface CopyButtonProps {
  // The text to copy
  value: string
  // Accessible name, e.g. "Copy trace ID"
  label?: string
  className?: string
  size?: 'icon-sm' | 'icon'
}

// How long the check mark replaces the copy icon
const COPIED_MS = 1500

// Copies value to the clipboard and confirms with a check mark
export function CopyButton({ value, label = 'Copy', className, size = 'icon-sm' }: CopyButtonProps) {
  const [status, setStatus] = useState<'idle' | 'copied' | 'denied'>('idle')

  useEffect(() => {
    if (status === 'idle') {
      return
    }
    const timer = setTimeout(() => setStatus('idle'), status === 'copied' ? COPIED_MS : 6000)
    return () => clearTimeout(timer)
  }, [status])

  const copy = async (event: React.MouseEvent) => {
    // Copy buttons often sit in clickable rows
    event.stopPropagation()
    try {
      await navigator.clipboard.writeText(value)
      setStatus('copied')
    } catch {
      setStatus('denied')
    }
  }

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size={size}
        className={cn('text-muted-foreground', className)}
        onClick={copy}
        aria-label={status === 'copied' ? 'Copied' : label}
        title={status === 'copied' ? 'Copied' : label}
      >
        {status === 'copied' ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </Button>
      {status === 'denied' && <span role="status" className="text-xs">Clipboard unavailable. Select and copy the text manually.</span>}
    </>
  )
}
