<script setup lang="ts">
import { ArcElement, Chart as ChartJS, Legend, Tooltip } from 'chart.js'
import type { ChartData, ChartOptions } from 'chart.js'
import { Doughnut } from 'vue-chartjs'

ChartJS.register(ArcElement, Tooltip, Legend)

defineProps<{
  data: ChartData<'doughnut'>
  options: ChartOptions<'doughnut'>
}>()
</script>

<template>
  <Doughnut :data="data" :options="options" />
</template>
