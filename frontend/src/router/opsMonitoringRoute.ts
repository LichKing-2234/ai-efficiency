export async function loadOpsMonitoringView() {
  const module = await import('@/views/admin/OpsMonitoringView.vue')
  return module.default
}
