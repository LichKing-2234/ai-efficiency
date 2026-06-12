import type { LucideIcon } from 'lucide-react'
import type * as React from 'react'
import { cloneElement, isValidElement } from 'react'
import { Button } from '@/components/ui/button'
import type { buttonVariants } from '@/components/ui/button'
import type { VariantProps } from 'class-variance-authority'

export function ButtonWithIcon({
  asChild = false,
  children,
  className,
  icon: Icon,
  iconPosition = 'start',
  size,
  variant,
  ...props
}: React.ComponentProps<'button'> & {
  asChild?: boolean
  children: React.ReactNode
  className?: string
  icon: LucideIcon
  iconPosition?: 'end' | 'start'
  size?: VariantProps<typeof buttonVariants>['size']
  variant?: VariantProps<typeof buttonVariants>['variant']
}) {
  const iconNode = <Icon data-icon={iconPosition === 'end' ? 'inline-end' : 'inline-start'} />

  if (asChild) {
    if (!isValidElement(children)) return null
    const child = children as React.ReactElement<{ children?: React.ReactNode }>
    return (
      <Button asChild className={className} size={size} variant={variant} {...props}>
        {cloneElement(child, {
          children: (
            <>
              {iconPosition === 'end' ? null : iconNode}
              {child.props.children}
              {iconPosition === 'end' ? iconNode : null}
            </>
          )
        })}
      </Button>
    )
  }

  return (
    <Button className={className} size={size} variant={variant} {...props}>
      {iconPosition === 'end' ? null : iconNode}
      {children}
      {iconPosition === 'end' ? iconNode : null}
    </Button>
  )
}
