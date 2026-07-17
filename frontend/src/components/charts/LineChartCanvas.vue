<script setup lang="ts">
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
} from 'chart.js'
import type { ChartData, ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

defineProps<{
  data: ChartData<'line'>
  options: ChartOptions<'line'>
}>()
</script>

<template>
  <Line :data="data" :options="options" />
</template>
