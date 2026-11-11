import { useId } from 'react'
import { ChevronLeftIcon, ChevronRightIcon, ChevronsLeftIcon } from 'lucide-react'
import { Pagination, PaginationContent, PaginationItem } from '@/components/ui/pagination'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { formatCount } from '@/lib/format'

const DEFAULT_PAGE_SIZE_OPTIONS = [10, 25, 50, 100]

interface DataPaginationProps {
  page: number
  pageSize: number
  // Rows on the current page
  itemCount: number
  total: number
  hasNext: boolean
  onFirst: () => void
  onPrevious: () => void
  onNext: () => void
  onPageSizeChange: (size: number) => void
  pageSizeOptions?: number[]
}

// Pagination controls for cursor-paginated lists (see usePagination): pages
// are visited in order, so there is no jump to the last page. The controls
// are buttons (they change the list, not the URL), styled as shadcn's
// pagination links.
export function DataPagination({
  page,
  pageSize,
  itemCount,
  total,
  hasNext,
  onFirst,
  onPrevious,
  onNext,
  onPageSizeChange,
  pageSizeOptions = DEFAULT_PAGE_SIZE_OPTIONS,
}: DataPaginationProps) {
  const pageSizeId = useId()
  const isFirstPage = page <= 1

  // Don't render if no data
  if (total === 0) {
    return null
  }

  const startItem = (page - 1) * pageSize + 1
  const endItem = startItem + Math.max(itemCount, 1) - 1
  const totalPages = Math.max(page, Math.ceil(total / pageSize))

  return (
    <div className="flex flex-col gap-3 pt-4 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Label htmlFor={pageSizeId} className="font-normal text-muted-foreground">
          Rows per page
        </Label>
        <NativeSelect
          id={pageSizeId}
          value={pageSize.toString()}
          onChange={(e) => onPageSizeChange(Number(e.target.value))}
          className="h-10 w-20"
        >
          {pageSizeOptions.map((size) => (
            <NativeSelectOption key={size} value={size}>
              {size}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm text-muted-foreground" aria-live="polite" aria-atomic="true">
          {formatCount(startItem)}–{formatCount(endItem)} of {formatCount(total)}
        </span>

        <Pagination className="mx-0 w-auto">
          <PaginationContent>
            <PaginationItem>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label="Go to first page"
                onClick={onFirst}
                disabled={isFirstPage}
              >
                <ChevronsLeftIcon className="h-4 w-4" />
              </Button>
            </PaginationItem>
            <PaginationItem>
              <Button
                type="button"
                variant="ghost"
                className="gap-1 px-2.5 sm:pl-2.5"
                aria-label="Go to previous page"
                onClick={onPrevious}
                disabled={isFirstPage}
              >
                <ChevronLeftIcon />
                <span className="hidden sm:block">Previous</span>
              </Button>
            </PaginationItem>
            <PaginationItem>
              <span className="flex h-10 items-center px-2 text-sm whitespace-nowrap">
                Page {formatCount(page)} of {formatCount(totalPages)}
              </span>
            </PaginationItem>
            <PaginationItem>
              <Button
                type="button"
                variant="ghost"
                className="gap-1 px-2.5 sm:pr-2.5"
                aria-label="Go to next page"
                onClick={onNext}
                disabled={!hasNext}
              >
                <span className="hidden sm:block">Next</span>
                <ChevronRightIcon />
              </Button>
            </PaginationItem>
          </PaginationContent>
        </Pagination>
      </div>
    </div>
  )
}
