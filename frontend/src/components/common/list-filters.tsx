import type { ReactNode } from 'react'
import { RefreshCw, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { DateRangePicker } from '@/components/common/date-range-picker'
import { useServiceNames } from '@/hooks/useServiceNames'
import { formatCount } from '@/lib/format'
import { cn } from '@/lib/utils'
import type { TimeSelection } from '@/types/dashboard'

// Filter controls shared by the list pages (logs, traces, sessions). The
// bar stacks the controls below md, with full-width inputs, and wraps them
// in a row from md.

export function FilterBar({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-3 md:flex-row md:flex-wrap md:items-center md:gap-4', className)}>{children}</div>
  )
}

interface TimeRangeFilterProps {
  id: string
  value: TimeSelection
  onChange: (selection: TimeSelection) => void
}

export function TimeRangeFilter({ id, value, onChange }: TimeRangeFilterProps) {
  return (
    <div className="flex flex-col gap-1.5 md:flex-row md:items-center md:gap-2">
      <Label htmlFor={id} className="font-normal whitespace-nowrap text-muted-foreground">
        Time range
      </Label>
      <DateRangePicker id={id} label="Time range" value={value} onChange={onChange} className="w-full md:w-auto" />
    </div>
  )
}

interface ServiceFilterProps {
  value: string
  onChange: (service: string) => void
  // Services that sent telemetry (useServices)
  services: string[]
}

export function ServiceFilter({ value, onChange, services }: ServiceFilterProps) {
  const { displayName, filterOptions } = useServiceNames()
  const options = filterOptions(services)
  // The selected service (e.g. from the URL) stays shown while the services
  // load, and when it has not sent telemetry
  if (value && !options.some((o) => o.value === value)) {
    options.push({ value, label: displayName(value) })
  }
  return (
    <div className="w-full md:w-48 *:w-full">
      <NativeSelect aria-label="Service" className="h-10" value={value} onChange={(e) => onChange(e.target.value)}>
        <NativeSelectOption value="">All Services</NativeSelectOption>
        {options.map((o) => (
          <NativeSelectOption key={o.value} value={o.value}>
            {o.label}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    </div>
  )
}

interface SearchFilterProps {
  // Accessible name, e.g. "Search logs"
  label: string
  placeholder: string
  value: string
  onChange: (value: string) => void
  // Marks the input invalid (e.g. a search the API rejected)
  invalid?: boolean
  // id of an element describing the input (e.g. the error)
  describedBy?: string
}

export function SearchFilter({ label, placeholder, value, onChange, invalid, describedBy }: SearchFilterProps) {
  return (
    <div className="relative w-full md:min-w-[12rem] md:flex-1">
      <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
      <Input
        type="search"
        aria-label={label}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="pl-9"
      />
    </div>
  )
}

interface NewItemsButtonProps {
  count: number
  // Singular and plural, e.g. ['log', 'logs']
  noun: [string, string]
  onClick: () => void
}

// "N new logs": shows the items that arrived since the list was loaded
export function NewItemsButton({ count, noun, onClick }: NewItemsButtonProps) {
  return (
    <Button variant="outline" size="sm" onClick={onClick} className="gap-2">
      <RefreshCw className="h-3 w-3" aria-hidden="true" />
      {formatCount(count)} new {count === 1 ? noun[0] : noun[1]}
    </Button>
  )
}
