import { cva, type VariantProps } from 'class-variance-authority'
import { Slot } from 'radix-ui'
import type * as React from 'react'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  "inline-flex shrink-0 cursor-pointer select-none items-center justify-center whitespace-nowrap rounded-[var(--r-md)] border border-transparent font-semibold text-[13px] outline-none transition-all duration-150 ease-[var(--ease-out)] focus-visible:shadow-[var(--sh-focus)] disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground shadow-[var(--sh-sm)] hover:-translate-y-px hover:shadow-[var(--sh-md)] active:translate-y-0',
        outline: 'border-border bg-card text-[var(--ink-2)] shadow-[var(--sh-sm)] hover:border-[var(--line-strong)] hover:bg-[var(--surface-2)] hover:text-foreground',
        secondary: 'border-border bg-secondary text-secondary-foreground hover:bg-accent',
        ghost: 'border-border bg-card text-[var(--ink-2)] shadow-[var(--sh-sm)] hover:border-[var(--line-strong)] hover:bg-[var(--surface-2)] hover:text-foreground',
        destructive: 'bg-destructive/10 text-destructive hover:bg-destructive/20',
        link: 'h-auto rounded-none border-0 px-0 text-[var(--ai-deep)] shadow-none hover:gap-2 hover:bg-transparent hover:underline-0'
      },
      size: {
        default: 'h-9 gap-[7px] px-[15px]',
        sm: 'h-8 gap-1.5 px-3 text-xs',
        lg: 'h-10 gap-2 px-4',
        icon: 'size-[30px] rounded-[var(--r-sm)] p-0 text-[var(--ink-3)] hover:bg-[var(--surface-3)] hover:text-foreground',
        'icon-sm': 'size-8 rounded-[var(--r-sm)] p-0 text-[var(--ink-3)] hover:bg-[var(--surface-3)] hover:text-foreground'
      }
    },
    defaultVariants: {
      variant: 'default',
      size: 'default'
    }
  }
)

function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
  }) {
  const Comp = asChild ? Slot.Root : 'button'
  return (
    <Comp
      data-slot='button'
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )
}

export { Button, buttonVariants }
