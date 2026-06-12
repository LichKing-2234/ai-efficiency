import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { InsetFieldList } from '@/components/primitives/inset-field-list'
import { cn } from '@/lib/utils'
import { CodeBlock } from './code-block'
import { FieldItem } from './field-list'

const advancedCodeClass = 'mt-3'

export type AdvancedDataField = {
  label: React.ReactNode
  mono?: boolean
  value: React.ReactNode
}

function AdvancedDataCode({ ariaLabel, children }: { ariaLabel?: string; children: string }) {
  return (
    <CodeBlock ariaLabel={ariaLabel} className={advancedCodeClass}>
      {children}
    </CodeBlock>
  )
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
      className={cn('rounded-[var(--r-md)] border border-border px-[14px]', className)}
      collapsible
      data-slot='advanced-data-panel'
      defaultValue={defaultOpen ? 'advanced' : undefined}
      type='single'
    >
      <AccordionItem value='advanced'>
        <AccordionTrigger>{title}</AccordionTrigger>
        <AccordionContent>
          <CardContentStack className='px-0 pb-0 text-[12.5px]' dataSlot='advanced-data-panel-content' gap='compact'>
            <InsetFieldList>
              {fields.map((field, index) => (
                <FieldItem key={index} label={field.label} mono={field.mono} value={field.value} />
              ))}
            </InsetFieldList>
          </CardContentStack>
          {code ? <AdvancedDataCode ariaLabel={codeAriaLabel}>{code}</AdvancedDataCode> : null}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  )
}
