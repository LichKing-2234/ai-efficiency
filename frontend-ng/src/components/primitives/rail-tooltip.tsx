import type * as React from 'react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

export function RailTooltip({
  children,
  content
}: {
  children: React.ReactNode
  content: React.ReactNode
}) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild data-slot='rail-tooltip-trigger'>
          <span data-slot='rail-tooltip'>{children}</span>
        </TooltipTrigger>
        <TooltipContent className='hidden group-data-[collapsed=true]/sidebar-wrapper:inline-flex' side='right' sideOffset={10}>
          {content}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
