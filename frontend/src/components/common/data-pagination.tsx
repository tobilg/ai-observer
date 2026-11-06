import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationFirst,
  PaginationPrevious,
  PaginationNext,
} from '@/components/ui/pagination'
import { Select } from '@/components/ui/select'

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
// are visited in order, so there is no jump to the last page
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
  const isFirstPage = page <= 1

  // Don't render if no data
  if (total === 0) {
    return null
  }

  const startItem = (page - 1) * pageSize + 1
  const endItem = startItem + Math.max(itemCount, 1) - 1
  const totalPages = Math.max(page, Math.ceil(total / pageSize))

  const handleClick = (action: () => void) => (e: React.MouseEvent) => {
    e.preventDefault()
    action()
  }

  return (
    <div className="flex items-center justify-between gap-4 pt-4">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <span>Rows per page:</span>
        <Select
          value={pageSize.toString()}
          onChange={(e) => onPageSizeChange(Number(e.target.value))}
          className="w-20"
        >
          {pageSizeOptions.map((size) => (
            <option key={size} value={size}>
              {size}
            </option>
          ))}
        </Select>
      </div>

      <div className="flex items-center gap-2">
        <span className="text-sm text-muted-foreground">
          {startItem}-{endItem} of {total}
        </span>

        <Pagination className="mx-0 w-auto">
          <PaginationContent>
            <PaginationItem>
              <PaginationFirst
                href="#"
                onClick={handleClick(onFirst)}
                disabled={isFirstPage}
              />
            </PaginationItem>
            <PaginationItem>
              <PaginationPrevious
                href="#"
                onClick={handleClick(onPrevious)}
                disabled={isFirstPage}
              />
            </PaginationItem>
            <PaginationItem>
              <span className="flex h-10 items-center px-2 text-sm">
                Page {page} of {totalPages}
              </span>
            </PaginationItem>
            <PaginationItem>
              <PaginationNext
                href="#"
                onClick={handleClick(onNext)}
                disabled={!hasNext}
              />
            </PaginationItem>
          </PaginationContent>
        </Pagination>
      </div>
    </div>
  )
}
