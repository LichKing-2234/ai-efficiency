import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { cn } from '@/lib/utils'
import { CodeBlock } from './code-block'
import { FieldItem, FieldList } from './field-list'

export type AdvancedDataField = {
  label: React.ReactNode
  mono?: boolean
  value: React.ReactNode
}

export function AdvancedDataPanel({
  className,
  code,
  codeAriaLabel,
  defaultOpen = false,
  fields,
  title
}: {
  className?: string
  code?: string | null
  codeAriaLabel?: string
  defaultOpen?: boolean
  fields: AdvancedDataField[]
  title: React.ReactNode
}) {
  return (
    <Accordion
      className={cn('rounded-[var(--r-md)] border border-border px-3', className)}
      collapsible
      data-slot='advanced-data-panel'
      defaultValue={defaultOpen ? 'advanced' : undefined}
      type='single'
    >
      <AccordionItem value='advanced'>
        <AccordionTrigger>{title}</AccordionTrigger>
        <AccordionContent>
          <div className='grid gap-2 text-sm'>
            <FieldList>
              {fields.map((field, index) => (
                <FieldItem key={index} label={field.label} mono={field.mono} value={field.value} />
              ))}
            </FieldList>
          </div>
          {code ? <CodeBlock ariaLabel={codeAriaLabel} className='mt-3'>{code}</CodeBlock> : null}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  )
}
