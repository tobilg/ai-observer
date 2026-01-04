import * as React from 'react'
import { CalendarIcon } from 'lucide-react'
import type { DateRange } from 'react-day-picker'
import { toast } from 'sonner'

import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select } from '@/components/ui/select'
import {
  TIMEFRAME_OPTIONS,
  getTimeSelectionLabel,
} from '@/types/dashboard'
import type { TimeSelection } from '@/types/dashboard'
import { calculateInterval, calculateTickInterval, isRangeExceeding } from '@/lib/timeUtils'

interface DateRangePickerProps {
  value: TimeSelection
  onChange: (selection: TimeSelection) => void
  className?: string
  align?: 'start' | 'center' | 'end'
}

export function DateRangePicker({ value, onChange, className, align = 'start' }: DateRangePickerProps) {
  const [open, setOpen] = React.useState(false)
  const [dateRange, setDateRange] = React.useState<DateRange | undefined>(
    value.type === 'absolute' ? { from: value.range.from, to: value.range.to } : undefined
  )

  // Reset date range when switching to relative
  React.useEffect(() => {
    if (value.type === 'relative') {
      setDateRange(undefined)
    }
  }, [value.type])

  const handlePresetChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const selectedValue = e.target.value
    if (selectedValue === 'custom') {
      // Don't change anything, just let user pick from calendar
      return
    }

    const timeframe = TIMEFRAME_OPTIONS.find((t) => t.value === selectedValue)
    if (timeframe) {
      setDateRange(undefined)
      onChange({
        type: 'relative',
        timeframe,
      })
      setOpen(false)
    }
  }

  const handleDateRangeSelect = (range: DateRange | undefined) => {
    setDateRange(range)

    if (range?.from && range?.to) {
      // Set from to start of day and to to end of day
      const from = new Date(range.from)
      from.setHours(0, 0, 0, 0)

      const to = new Date(range.to)
      to.setHours(23, 59, 59, 999)

      // Check for performance warning
      if (isRangeExceeding(180, from, to)) {
        toast.warning('Large date range selected', {
          description: 'Queries over 180 days may take longer to load.',
        })
      }

      const intervalSeconds = calculateInterval(from, to)
      const rangeDays = (to.getTime() - from.getTime()) / (1000 * 60 * 60 * 24)
      const bucketCount = Math.ceil((rangeDays * 86400) / intervalSeconds)
      const tickInterval = calculateTickInterval(bucketCount)

      // Show static data toast
      toast.info('Fixed date range', {
        description: 'Data will not auto-refresh. Select a relative range for live updates.',
      })

      onChange({
        type: 'absolute',
        range: {
          from,
          to,
          intervalSeconds,
          tickInterval,
        },
      })
      setOpen(false)
    }
  }

  const currentValue = value.type === 'relative' ? value.timeframe.value : 'custom'

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          className={cn(
            'justify-start text-left font-normal min-w-[180px]',
            !value && 'text-muted-foreground',
            className
          )}
        >
          <CalendarIcon className="mr-2 h-4 w-4" />
          <span className="truncate">{getTimeSelectionLabel(value)}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align={align}>
        <div className="p-3 border-b">
          <Select
            size="sm"
            value={currentValue}
            onChange={handlePresetChange}
            className="w-full"
          >
            <optgroup label="Relative Time Ranges">
              {TIMEFRAME_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </optgroup>
            <optgroup label="Custom">
              <option value="custom">Custom date range...</option>
            </optgroup>
          </Select>
        </div>
        <Calendar
          mode="range"
          defaultMonth={dateRange?.from}
          selected={dateRange}
          onSelect={handleDateRangeSelect}
          numberOfMonths={1}
          disabled={{ after: new Date() }}
        />
      </PopoverContent>
    </Popover>
  )
}
