import type * as React from 'react'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { SparkBars } from '@/components/primitives/charts'
import { cn } from '@/lib/utils'

export function PulseStat({
  color,
  divider = false,
  label,
  value,
  values
}: {
  color: string
  divider?: boolean
  label: React.ReactNode
  value: React.ReactNode
  values?: number[]
}) {
  return (
    <div className={cn('min-w-[112px] flex-1', divider && 'border-[var(--line-faint)] border-l')}>
      <CardContentStack className='px-[16px] py-[12px]'>
        <div className='text-[11px] font-medium text-[var(--ink-3)]'>{label}</div>
        <div className='tnum mt-[3px] mb-[6px] text-[19px] font-[680] tracking-[-0.02em]'>{value}</div>
        {values?.length ? <SparkBars color={color} data={values.map((point) => ({ value: point }))} height={20} width={104} /> : null}
      </CardContentStack>
    </div>
  )
}
