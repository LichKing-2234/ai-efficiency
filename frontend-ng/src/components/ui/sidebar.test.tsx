import { HomeIcon } from 'lucide-react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import {
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
} from './sidebar'

describe('Sidebar', () => {
  test('renders expanded and collapsed shell slots with stable data contracts', () => {
    const html = renderToStaticMarkup(
      <SidebarProvider collapsed={false}>
        <SidebarLayout>
          <Sidebar>
            <SidebarHeader>
              <SidebarBrand mark='AE' title='AI Efficiency' subtitle='console · ng' />
            </SidebarHeader>
            <SidebarContent>
              <SidebarGroup>
                <SidebarGroupLabel>Analyze</SidebarGroupLabel>
                <SidebarGroupContent>
                  <SidebarMenu>
                    <SidebarMenuItem>
                      <SidebarMenuButton active icon={HomeIcon} tooltip='Overview'>
                        Overview
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  </SidebarMenu>
                </SidebarGroupContent>
              </SidebarGroup>
              <SidebarSeparator />
            </SidebarContent>
            <SidebarFooter>Account</SidebarFooter>
            <SidebarRail />
          </Sidebar>
          <SidebarInset>Content</SidebarInset>
        </SidebarLayout>
      </SidebarProvider>
    )

    expect(html).toContain('data-slot="sidebar-provider"')
    expect(html).toContain('data-collapsed="false"')
    expect(html).toContain('data-slot="sidebar-layout"')
    expect(html).toContain('data-slot="sidebar"')
    expect(html).toContain('style="--sidebar-width:var(--rail);--sidebar-width-icon:68px"')
    expect(html).toContain('AI Efficiency')
    expect(html).toContain('aria-current="page"')
    expect(html).toContain('data-active="true"')
    expect(html).toContain('data-tooltip="Overview"')
    expect(html).toContain('data-slot="sidebar-inset"')
  })

  test('hides labels and exposes tooltip labels when collapsed', () => {
    const html = renderToStaticMarkup(
      <SidebarProvider collapsed>
        <Sidebar>
          <SidebarHeader>
            <SidebarBrand mark='AE' title='AI Efficiency' subtitle='console · ng' />
          </SidebarHeader>
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupLabel>Analyze</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  <SidebarMenuItem>
                    <SidebarMenuButton icon={HomeIcon} tooltip='Overview'>
                      Overview
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          </SidebarContent>
        </Sidebar>
      </SidebarProvider>
    )

    expect(html).toContain('data-collapsed="true"')
    expect(html).toContain('AI Efficiency')
    expect(html).toContain('Analyze')
    expect(html).toContain('Overview')
    expect(html).toContain('group-data-[collapsed=true]/sidebar-wrapper:sr-only')
    expect(html).toContain('data-tooltip="Overview"')
  })

  test('keeps collapsed navigation buttons square and shadow-free', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./sidebar.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('flex h-[34px] w-full items-center gap-[10px]')
    expect(source).toContain('group-data-[collapsed=true]/sidebar-wrapper:size-[42px]')
    expect(source).toContain("active && 'border-border bg-sidebar-accent text-foreground'")
    expect(source).toContain("transition-[width] duration-200 ease-[var(--ease-out)] min-[920px]:flex")
    expect(source).not.toContain('shadow-[var(--sh-sm)]')
    expect(source).not.toContain('shadow-[var(--sh-lg)]')
    expect(source).not.toContain('shadow-[0_2px_8px_var(--ai-glow)]')
  })

  test('keeps collapsed brand and footer footprints square', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./sidebar.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('group-data-[collapsed=true]/sidebar-wrapper:size-[42px]')
    expect(source).toContain('group-data-[collapsed=true]/sidebar-wrapper:justify-center')
    expect(source).toContain("className={cn('flex h-[var(--topbar)] shrink-0 items-center border-b border-[var(--line-faint)] px-4 group-data-[collapsed=true]/sidebar-wrapper:justify-center group-data-[collapsed=true]/sidebar-wrapper:px-0', className)}")
    expect(source).toContain("className={cn('shrink-0 border-t border-[var(--line-faint)] p-3 group-data-[collapsed=true]/sidebar-wrapper:grid group-data-[collapsed=true]/sidebar-wrapper:place-items-center', className)}")
  })

  test('keeps collapsed tooltip copy out of the expanded accessible label', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./sidebar.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/app-brand'")
    expect(source).toContain("from '@/components/primitives/section-eyebrow'")
    expect(source).toContain("from '@/components/primitives/rail-tooltip'")
    expect(source).toContain('<AppBrand')
    expect(source).toContain('<SectionEyebrow')
    expect(source).toContain('<RailTooltip')
  })
})
