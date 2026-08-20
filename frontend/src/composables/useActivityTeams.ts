import { computed, onMounted } from 'vue'
import { useTeamUsageOrganization } from '@/composables/useTeamUsageOrganization'

export function useActivityTeams() {
  const organization = useTeamUsageOrganization()
  const loading = computed(() => Boolean(organization.rootBranch.value?.loading && !organization.rootBranch.value.loaded))
  const error = computed(() => Boolean(organization.rootBranch.value?.error && !organization.rootBranch.value.loaded))

  function load() {
    organization.reset({})
  }

  onMounted(load)

  return {
    loading,
    error,
    load,
    rootBranch: organization.rootBranch,
    branchFor: organization.branchFor,
    ensureBranch: organization.ensureBranch,
    loadMoreDepartments: organization.loadMoreDepartments,
  }
}
