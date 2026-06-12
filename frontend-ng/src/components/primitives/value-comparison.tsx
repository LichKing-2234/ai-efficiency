export function ValueComparison({
  current,
  previous
}: {
  current: React.ReactNode
  previous?: React.ReactNode
}) {
  return (
    <div className='flex items-baseline gap-3' data-slot='value-comparison'>
      <div className='tnum text-3xl font-semibold leading-none'>{current}</div>
      {previous ? <div className='text-[12px] text-[var(--ink-3)] line-through'>{previous}</div> : null}
    </div>
  )
}
