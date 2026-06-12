export function ValueComparison({
  current,
  previous
}: {
  current: React.ReactNode
  previous?: React.ReactNode
}) {
  return (
    <div className='flex items-baseline gap-3' data-slot='value-comparison'>
      <div className='tnum text-[30px] font-[680] leading-none tracking-[-0.02em]'>{current}</div>
      {previous ? <div className='text-[12px] text-[var(--ink-3)] line-through'>{previous}</div> : null}
    </div>
  )
}
