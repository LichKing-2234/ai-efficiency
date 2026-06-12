import type * as React from 'react'
import { CardFilterBar } from '@/components/primitives/card-filter-bar'
import { EndActions } from '@/components/primitives/end-actions'
import { FilterRow } from '@/components/primitives/filter-row'

export function SearchActionBar({
  actions,
  children,
  search
}: {
  actions?: React.ReactNode
  children?: React.ReactNode
  search: React.ReactNode
}) {
  return (
    <CardFilterBar stacked={Boolean(children)}>
      <FilterRow className='min-w-0' dataSlot='search-action-bar' justify='between'>
        <div className='min-w-0 flex-1' data-slot='search-action-bar-search'>
          {search}
        </div>
        {actions ? (
          <EndActions>
            <div data-slot='search-action-bar-actions'>{actions}</div>
          </EndActions>
        ) : null}
      </FilterRow>
      {children}
    </CardFilterBar>
  )
}
