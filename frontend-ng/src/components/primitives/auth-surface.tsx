import type * as React from 'react'
import { AppBrand } from '@/components/primitives/app-brand'
import { AuthSurfaceActions } from '@/components/primitives/auth-surface-actions'
import { AuthSurfaceFrame } from '@/components/primitives/auth-surface-frame'

export function AuthSurface({
  actions,
  aside,
  children,
  className,
  description,
  title
}: {
  actions?: React.ReactNode
  aside?: React.ReactNode
  children: React.ReactNode
  className?: string
  description: string
  title: string
}) {
  return (
    <main
      data-slot='auth-surface'
      className='grid min-h-screen place-items-center overflow-x-hidden bg-[radial-gradient(120%_140%_at_88%_-10%,var(--ai-softer),transparent_55%),var(--bg)] px-[18px] py-[22px]'
    >
      <div className='flex w-full max-w-[448px] flex-col gap-[12px]'>
        <AppBrand
          className='justify-center text-center'
          compact
          mark={<span className='font-[700] leading-none'>AI</span>}
          subtitle='console · ng'
          title='AI Efficiency'
        />

        <AuthSurfaceFrame aside={aside} className={className} description={description} title={title}>
          {children}
        </AuthSurfaceFrame>
        {actions ? <AuthSurfaceActions>{actions}</AuthSurfaceActions> : null}

        <p
          data-slot='auth-surface-caption'
          className='px-[6px] text-center text-[11px] leading-[1.45] text-[var(--ink-4)]'
        >
          Same-origin auth bridge. Local dev reuses the current online session through secure cookies.
        </p>
      </div>
    </main>
  )
}
