import type * as React from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

type SidebarProviderProps = React.ComponentProps<'div'> & {
  collapsed?: boolean
  width?: string
  iconWidth?: string
}

function SidebarProvider({
  className,
  collapsed = false,
  width = 'var(--rail)',
  iconWidth = '68px',
  style,
  ...props
}: SidebarProviderProps) {
  return (
    <div
      data-collapsed={collapsed}
      data-slot='sidebar-provider'
      style={{ '--sidebar-width': width, '--sidebar-width-icon': iconWidth, ...style } as React.CSSProperties}
      className={cn('group/sidebar-wrapper has-data-[collapsed=true]:[--sidebar-current-width:var(--sidebar-width-icon)] has-data-[collapsed=false]:[--sidebar-current-width:var(--sidebar-width)]', className)}
      {...props}
    />
  )
}

function SidebarLayout({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='sidebar-layout' className={cn('flex h-screen overflow-hidden bg-background text-foreground', className)} {...props} />
}

function Sidebar({ className, ...props }: React.ComponentProps<'aside'>) {
  return (
    <aside
      data-slot='sidebar'
      className={cn(
        'hidden h-full w-[var(--sidebar-current-width)] shrink-0 flex-col overflow-hidden border-r border-sidebar-border bg-sidebar text-sidebar-foreground transition-[width] duration-200 ease-[var(--ease-out)] min-[920px]:flex',
        className
      )}
      {...props}
    />
  )
}

function SidebarInset({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='sidebar-inset' className={cn('flex min-w-0 flex-1 flex-col', className)} {...props} />
}

function SidebarHeader({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='sidebar-header' className={cn('flex h-[var(--topbar)] shrink-0 items-center border-b border-[var(--line-faint)] px-4 group-data-[collapsed=true]/sidebar-wrapper:justify-center group-data-[collapsed=true]/sidebar-wrapper:px-0', className)} {...props} />
}

function SidebarContent({ className, ...props }: React.ComponentProps<'nav'>) {
  return <nav data-slot='sidebar-content' className={cn('flex-1 overflow-y-auto overflow-x-hidden p-3', className)} {...props} />
}

function SidebarFooter({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='sidebar-footer' className={cn('shrink-0 border-t border-[var(--line-faint)] p-3 group-data-[collapsed=true]/sidebar-wrapper:grid group-data-[collapsed=true]/sidebar-wrapper:place-items-center', className)} {...props} />
}

function SidebarGroup({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='sidebar-group' className={cn('mb-4 group-data-[collapsed=true]/sidebar-wrapper:mb-2', className)} {...props} />
}

function SidebarGroupLabel({ className, children, ...props }: React.ComponentProps<'div'>) {
  return (
    <div data-slot='sidebar-group-label' className={cn('px-2 py-1 font-semibold text-[10px] text-[var(--ink-4)] uppercase tracking-[0.08em] group-data-[collapsed=true]/sidebar-wrapper:sr-only', className)} {...props}>
      {children}
    </div>
  )
}

function SidebarGroupContent({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot='sidebar-group-content' className={cn('flex flex-col gap-1', className)} {...props} />
}

function SidebarMenu({ className, ...props }: React.ComponentProps<'ul'>) {
  return <ul data-slot='sidebar-menu' className={cn('flex flex-col gap-1', className)} {...props} />
}

function SidebarMenuItem({ className, ...props }: React.ComponentProps<'li'>) {
  return <li data-slot='sidebar-menu-item' className={cn('min-w-0', className)} {...props} />
}

type SidebarMenuButtonRenderProps = React.HTMLAttributes<HTMLElement> & {
  'aria-current'?: React.AriaAttributes['aria-current']
  'data-active': boolean
  'data-slot': string
  'data-tooltip'?: string
  children: React.ReactNode
}

type SidebarMenuButtonProps = React.HTMLAttributes<HTMLElement> & {
  active?: boolean
  icon?: LucideIcon
  render?: (props: SidebarMenuButtonRenderProps) => React.ReactNode
  tooltip?: string
}

function SidebarMenuButton({
  active = false,
  children,
  className,
  icon: Icon,
  render,
  tooltip,
  ...props
}: SidebarMenuButtonProps) {
  const content = (
    <>
      {active ? <span className='absolute top-2 bottom-2 -left-3 w-[3px] rounded-full bg-[var(--ai)] group-data-[collapsed=true]/sidebar-wrapper:hidden' /> : null}
      {Icon ? <Icon className={cn('size-4 text-[var(--ink-3)] group-data-[collapsed=true]/sidebar-wrapper:size-[19px]', active && 'text-[var(--ai)]')} /> : null}
      <span className='truncate group-data-[collapsed=true]/sidebar-wrapper:sr-only'>{children}</span>
      {tooltip ? (
        <span aria-hidden='true' className='pointer-events-none absolute left-[calc(100%+10px)] top-1/2 z-50 hidden -translate-y-1/2 scale-95 whitespace-nowrap rounded-[var(--r-sm)] border border-[var(--line-strong)] bg-[var(--surface)] px-2 py-1 font-semibold text-[11px] text-[var(--ink-2)] opacity-0 transition group-hover/sidebar-item:scale-100 group-hover/sidebar-item:opacity-100 group-data-[collapsed=true]/sidebar-wrapper:block'>
          {tooltip}
        </span>
      ) : null}
    </>
  )
  const buttonProps = {
    'aria-current': active ? 'page' : undefined,
    'data-active': active,
    'data-slot': 'sidebar-menu-button',
    'data-tooltip': tooltip,
    title: tooltip,
    className: cn(
      'group/sidebar-item relative flex h-[34px] w-full items-center gap-[10px] rounded-[var(--r-sm)] border border-transparent px-[9px] text-[13.5px] font-medium text-[var(--ink-2)] shadow-none transition-colors duration-150 hover:bg-[var(--surface-2)] hover:text-foreground group-data-[collapsed=true]/sidebar-wrapper:size-[42px] group-data-[collapsed=true]/sidebar-wrapper:justify-center group-data-[collapsed=true]/sidebar-wrapper:px-0',
      active && 'border-border bg-sidebar-accent text-foreground',
      className
    ),
    ...props,
    children: content
  } satisfies SidebarMenuButtonRenderProps

  if (render) return render(buttonProps)
  return <button type='button' {...buttonProps as React.ComponentProps<'button'>} />
}

function SidebarSeparator({ className, ...props }: React.ComponentProps<'div'>) {
  return <div aria-hidden='true' data-slot='sidebar-separator' className={cn('mx-1 my-2 hidden h-px bg-[var(--line-faint)] group-data-[collapsed=true]/sidebar-wrapper:block', className)} {...props} />
}

function SidebarRail({ className, ...props }: React.ComponentProps<'div'>) {
  return <div aria-hidden='true' data-slot='sidebar-rail' className={cn('hidden', className)} {...props} />
}

type SidebarBrandProps = React.ComponentProps<'div'> & {
  mark: React.ReactNode
  title: React.ReactNode
  subtitle?: React.ReactNode
}

function SidebarBrand({ className, mark, title, subtitle, ...props }: SidebarBrandProps) {
  return (
    <div data-slot='sidebar-brand' className={cn('flex min-w-0 items-center gap-2 font-semibold', className)} {...props}>
      <span className='grid size-7 shrink-0 place-items-center rounded-[var(--r-sm)] bg-[linear-gradient(135deg,var(--ai-bright),var(--ai-deep))] text-primary-foreground text-xs'>{mark}</span>
      <span className='min-w-0 group-data-[collapsed=true]/sidebar-wrapper:sr-only'>
        <span className='block truncate'>{title}</span>
        {subtitle ? <span className='block font-mono text-[10px] text-[var(--ink-4)]'>{subtitle}</span> : null}
      </span>
    </div>
  )
}

export {
  Sidebar,
  SidebarBrand,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarLayout,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator
}
