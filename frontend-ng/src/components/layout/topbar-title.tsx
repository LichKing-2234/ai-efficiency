import type * as React from 'react'
import { SectionEyebrow } from '@/components/primitives/section-eyebrow'
import { cn } from '@/lib/utils'

export function TopbarTitle({
  className,
  section,
  title
}: {
  className?: string
  section: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <div className={cn('min-w-0', className)} data-slot='topbar-title'>
      <SectionEyebrow className='mb-0 text-[10.5px] tracking-[0.04em]' data-slot='topbar-title-section'>
        {section}
      </SectionEyebrow>
      <div className='truncate text-[15px] leading-[1.1] font-[650] tracking-[-0.01em]' data-slot='topbar-title-text'>
        {title}
      </div>
    </div>
  )
}
