export async function loadRelayPlanningView() {
  const module = await import('@/views/admin/RelayPlanningView.vue')
  return module.default
}
