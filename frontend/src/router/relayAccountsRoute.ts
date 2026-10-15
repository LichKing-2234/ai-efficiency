export async function loadRelayAccountsView() {
  const module = await import('@/views/admin/AdminRelayAccountsView.vue')
  return module.default
}
