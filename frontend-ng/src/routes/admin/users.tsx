import { createFileRoute, redirect } from '@tanstack/react-router'
import { AdminUsersPage } from '@/features/admin-users/admin-users-page'
import { queryClient } from '@/query-client'
import { ensureAuthenticatedUser } from '@/lib/auth/session'

export const Route = createFileRoute('/admin/users')({
  beforeLoad: async () => {
    const user = await queryClient.fetchQuery({ queryKey: ['auth', 'me'], queryFn: ensureAuthenticatedUser })
    if (user.role !== 'admin') throw redirect({ to: '/' })
  },
  component: AdminUsersPage
})
