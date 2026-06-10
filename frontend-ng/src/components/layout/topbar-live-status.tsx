export function TopbarLiveStatus({ label }: { label: string }) {
  return (
    <div
      className='hidden items-center gap-2 rounded-full border border-[var(--pos-line)] bg-[var(--pos-soft)] px-3 py-1 md:flex'
      data-slot='topbar-live-status'
    >
      <span className='live-dot' data-slot='topbar-live-status-dot' />
      <span className='font-semibold text-[11.5px] text-[var(--pos)]' data-slot='topbar-live-status-label'>
        {label}
      </span>
    </div>
  )
}
