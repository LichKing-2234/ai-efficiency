<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  text: string
  query: string
}>()

// Splits the text around every case-insensitive occurrence of the query so the
// matched part can be emphasised without injecting HTML.
const parts = computed(() => {
  const text = props.text ?? ''
  const query = props.query.trim()
  if (!query) return [{ text, match: false }]
  const lowerText = text.toLowerCase()
  const lowerQuery = query.toLowerCase()
  const result: { text: string; match: boolean }[] = []
  let cursor = 0
  while (cursor < text.length) {
    const index = lowerText.indexOf(lowerQuery, cursor)
    if (index < 0) {
      result.push({ text: text.slice(cursor), match: false })
      break
    }
    if (index > cursor) result.push({ text: text.slice(cursor, index), match: false })
    result.push({ text: text.slice(index, index + query.length), match: true })
    cursor = index + query.length
  }
  return result
})
</script>

<template>
  <span>
    <template v-for="(part, index) in parts" :key="index">
      <mark v-if="part.match" class="rounded-sm bg-blue-100 px-0 text-blue-800">{{ part.text }}</mark>
      <template v-else>{{ part.text }}</template>
    </template>
  </span>
</template>
