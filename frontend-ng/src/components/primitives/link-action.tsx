import type { LucideIcon } from 'lucide-react'
import type * as React from 'react'
import { cloneElement, isValidElement } from 'react'
import { Button } from '@/components/ui/button'

export function LinkAction({
  asChild = false,
  children,
  iconEnd: IconEnd,
  ...props
}: React.ComponentProps<typeof Button> & {
  asChild?: boolean
  children: React.ReactNode
  iconEnd?: LucideIcon
}) {
  if (asChild) {
    if (!isValidElement(children)) return null
    const child = children as React.ReactElement<{ children?: React.ReactNode }>
    return (
      <Button asChild size='sm' variant='link' {...props}>
        {cloneElement(child, {
          children: (
            <>
              {child.props.children}
              {IconEnd ? <IconEnd data-icon='inline-end' /> : null}
            </>
          )
        })}
      </Button>
    )
  }

  return (
    <Button size='sm' variant='link' {...props}>
      {children}
      {IconEnd ? <IconEnd data-icon='inline-end' /> : null}
    </Button>
  )
}
