import type * as React from 'react'
import { CardFilterBar } from '@/components/primitives/card-filter-bar'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { WorkbenchFrame } from '@/components/primitives/workbench-frame'
import { WorkbenchContent, WorkbenchRail } from '@/components/primitives/workbench-rail'

export function RepositoriesWorkbenchShell({
  children,
  footer,
  header,
  meta,
  providerTabs,
  rail,
  railActions,
  railDescription,
  railTitle,
  title
}: {
  children: React.ReactNode
  footer?: React.ReactNode
  header?: React.ReactNode
  meta?: React.ReactNode
  providerTabs: React.ReactNode
  rail: React.ReactNode
  railActions?: React.ReactNode
  railDescription?: React.ReactNode
  railTitle: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <WorkbenchFrame
      body={(
        <div className='repo-workbench' data-slot='repositories-workbench-grid'>
          <WorkbenchRail
            actions={railActions}
            description={railDescription}
            title={railTitle}
          >
            {rail}
          </WorkbenchRail>
          <WorkbenchContent>
            <SectionCardHeader
              actions={header}
              description={railDescription}
              meta={meta}
              title={title}
            />
            {children}
          </WorkbenchContent>
        </div>
      )}
      footer={footer}
      topBar={(
        <CardFilterBar>
          {providerTabs}
        </CardFilterBar>
      )}
    />
  )
}
