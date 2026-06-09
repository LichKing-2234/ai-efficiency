import type * as React from 'react'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { cn } from '@/lib/utils'

export function CommandAccordion({
  children,
  className,
  defaultOpen = false,
  title
}: {
  children: React.ReactNode
  className?: string
  defaultOpen?: boolean
  title: React.ReactNode
}) {
  return (
    <Accordion className={cn('mt-2 rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] px-3', className)} collapsible data-slot='command-accordion' defaultValue={defaultOpen ? 'content' : undefined} type='single'>
      <AccordionItem value='content'>
        <AccordionTrigger>{title}</AccordionTrigger>
        <AccordionContent>{children}</AccordionContent>
      </AccordionItem>
    </Accordion>
  )
}
