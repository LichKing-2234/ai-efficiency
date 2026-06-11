import type * as React from 'react'
import { cn } from '@/lib/utils'

type CardProps = React.ComponentProps<'div'> & {
  variant?: 'default' | 'accent'
}

const cardVariants = {
  default: '',
  accent: 'grid-paper overflow-hidden border-[var(--ai-line)] bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]'
} satisfies Record<NonNullable<CardProps['variant']>, string>

function Card({ className, variant = 'default', ...props }: CardProps) {
  return (
    <div
      data-slot='card'
      className={cn(
        'rounded-[var(--r-lg)] border border-border bg-card text-card-foreground',
        cardVariants[variant],
        className
      )}
      {...props}
    />
  )
}

function CardHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='card-header' className={cn('flex flex-col gap-1 p-[18px] pb-2', className)} {...props} />
}

function CardTitle({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='card-title' className={cn('font-[650] text-[14px] leading-none', className)} {...props} />
}

function CardDescription({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='card-description' className={cn('text-[12px] text-[var(--ink-3)]', className)} {...props} />
}

function CardContent({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='card-content' className={cn('p-[18px] pt-2', className)} {...props} />
}

function CardFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='card-footer' className={cn('flex items-center border-t border-border p-[18px]', className)} {...props} />
}

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter }
