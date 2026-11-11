import { describe, it, expect } from 'vitest'
import { screen } from '@testing-library/react'
import { DatabaseCard } from '../DatabaseCard'
import { renderWithProviders } from '@/test/queryWrapper'
import { statusFixture } from '@/test/statusFixture'
import type { StatusResponse } from '@/types/status'

const MB = 1024 ** 2

function status(database: Partial<StatusResponse['database']>, settings: Partial<StatusResponse['settings']> = {}): StatusResponse {
  return statusFixture({ database: { sizeBytes: 750 * MB, ...database }, settings })
}

describe('DatabaseCard', () => {
  it('shows the data, free space and growth, and suggests compaction', () => {
    renderWithProviders(
      <DatabaseCard
        status={status({ usedBytes: 600 * MB, freeBytes: 150 * MB, compactRecommended: true, growthBytesPerDay: 350 * MB })}
      />,
    )
    expect(screen.getByText(/150.0 MB free inside the file, 20%/)).toBeInTheDocument()
    expect(screen.getByText(/^about 350.0 MB a day/)).toBeInTheDocument()
    expect(screen.getByText('All telemetry is kept')).toBeInTheDocument()
    expect(screen.getByText(/grows by about 350.0 MB a day/)).toBeInTheDocument()
    expect(screen.getByText('150.0 MB of the file are free')).toBeInTheDocument()
    expect(screen.getByText('ai-observer compact')).toBeInTheDocument()
  })

  it('leaves out the hints when growth is bounded and little is free', () => {
    renderWithProviders(
      <DatabaseCard status={status({ usedBytes: 740 * MB, freeBytes: 10 * MB, compactRecommended: false }, { retentionDays: 30 })} />,
    )
    expect(screen.queryByText('All telemetry is kept')).not.toBeInTheDocument()
    expect(screen.queryByText(/of the file are free/)).not.toBeInTheDocument()
    expect(screen.getByText('Known after a day of telemetry')).toBeInTheDocument()
  })

  it('estimates when the size limit is reached', () => {
    renderWithProviders(
      <DatabaseCard
        status={status({ usedBytes: 1000 * MB, freeBytes: 0, growthBytesPerDay: 100 * MB }, { maxDbSizeBytes: 2000 * MB })}
      />,
    )
    expect(screen.getByText(/size limit in about 10 days/)).toBeInTheDocument()
    expect(screen.queryByText('All telemetry is kept')).not.toBeInTheDocument()
  })
})
