import { Link, useLocation } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/common/empty-state'
import { PageHeader } from '@/components/common/page-header'

export function NotFoundPage() {
  const { pathname } = useLocation()
  return (
    <div className="space-y-6">
      <PageHeader title="Page not found" />
      <EmptyState
        title="There is no page at this address"
        description={<span className="font-mono">{pathname}</span>}
        action={
          <Button asChild>
            <Link to="/">Go to the dashboard</Link>
          </Button>
        }
      />
    </div>
  )
}
