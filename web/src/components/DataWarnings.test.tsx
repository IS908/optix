import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { DataWarnings } from './DataWarnings'

describe('DataWarnings', () => {
  it('contains long warning text within the card', () => {
    render(
      <DataWarnings
        warnings={[
          'option stress: https://query2.finance.yahoo.com/v6/finance/quoteSummary/SPY?modules=financialData&modules=quoteType&modules=defaultKeyStatistics',
        ]}
      />,
    )

    const list = screen.getByRole('list')
    expect(list.className).toContain('max-h-40')
    expect(list.className).toContain('overflow-auto')
    expect(screen.getByRole('listitem').className).toContain('break-all')
  })
})
