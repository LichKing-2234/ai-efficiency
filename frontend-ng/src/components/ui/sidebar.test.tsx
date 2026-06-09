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
})
