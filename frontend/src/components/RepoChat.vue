<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { CodeDiff } from 'v-code-diff'
import { sendChatMessage } from '@/api/chat'
import { optimizePreview, optimizeConfirm } from '@/api/analysis'
import type { ChatMessage, ChatToolCall } from '@/api/chat'
import type { OptimizePreviewFile, OptimizeResult } from '@/api/analysis'

const props = defineProps<{ repoId: number }>()

const open = ref(false)
const messages = ref<ChatMessage[]>([])
const input = ref('')
const loading = ref(false)
const error = ref('')
const messagesEl = ref<HTMLElement | null>(null)

// Optimize preview state
const previewMode = ref(false)
const previewLoading = ref(false)
const previewFiles = ref<OptimizePreviewFile[]>([])
const previewScore = ref(0)
const confirmLoading = ref(false)
const confirmResult = ref<OptimizeResult | null>(null)
const expandedFiles = ref<Set<string>>(new Set())

function toggle() {
  open.value = !open.value
}

// Called from parent to start optimize preview flow
async function startOptimizePreview() {
  open.value = true
  previewMode.value = true
  previewLoading.value = true
  previewFiles.value = []
  confirmResult.value = null
  error.value = ''

  // Inject context message
  messages.value.push({
    role: 'assistant',
    content: 'Generating optimization preview... I\'ll show you the proposed changes before creating a PR.',
  })
  await scrollToBottom()

  try {
    const res = await optimizePreview(props.repoId)
    const data = res.data?.data
    if (!data || !data.files || data.files.length === 0) {
      messages.value.push({
        role: 'assistant',
        content: 'No auto-fixable issues found. All files are already up to date.',
      })
      previewMode.value = false
      return
    }
    previewFiles.value = data.files
    previewScore.value = data.score
    // Expand all files by default
    expandedFiles.value = new Set(data.files.map((f: OptimizePreviewFile) => f.path))
    messages.value.push({
      role: 'assistant',
      content: `Found ${data.files.length} file(s) to optimize (current score: ${data.score}/100). Review the changes below, then click "Submit PR" to create the pull request. You can also ask me to modify the content.`,
    })
  } catch (e: any) {
    const msg = e.response?.data?.message || 'Preview failed'
    error.value = msg
    previewMode.value = false
  } finally {
    previewLoading.value = false
    await scrollToBottom()
  }
}

async function handleConfirm() {
  confirmLoading.value = true
  error.value = ''
  try {
    const files: Record<string, string> = {}
    for (const f of previewFiles.value) {
      files[f.path] = f.new_content
    }
    const res = await optimizeConfirm(props.repoId, files, previewScore.value)
    confirmResult.value = res.data?.data ?? null
    previewMode.value = false
    previewFiles.value = []
  } catch (e: any) {
    error.value = e.response?.data?.message || 'Failed to create PR'
  } finally {
    confirmLoading.value = false
    await scrollToBottom()
  }
}

function toggleFile(path: string) {
  if (expandedFiles.value.has(path)) {
    expandedFiles.value.delete(path)
  } else {
    expandedFiles.value.add(path)
  }
  expandedFiles.value = new Set(expandedFiles.value)
}

function removeFile(path: string) {
  previewFiles.value = previewFiles.value.filter(f => f.path !== path)
  expandedFiles.value.delete(path)
  expandedFiles.value = new Set(expandedFiles.value)
  messages.value.push({
    role: 'assistant',
    content: `Removed "${path}" from the PR. ${previewFiles.value.length} file(s) remaining.`,
  })
  if (previewFiles.value.length === 0) {
    previewMode.value = false
    messages.value.push({
      role: 'assistant',
      content: 'All files removed. Nothing to submit.',
    })
  }
  scrollToBottom()
}

async function send() {
  const text = input.value.trim()
  if (!text || loading.value) return

  error.value = ''
  messages.value.push({ role: 'user', content: text })
  input.value = ''
  loading.value = true
  await scrollToBottom()

  try {
    // If in preview mode, send file context for function calling
    const pf = previewMode.value && previewFiles.value.length > 0
      ? previewFiles.value.map(f => ({ path: f.path, new_content: f.new_content }))
      : undefined
    const res = await sendChatMessage(props.repoId, text, messages.value.slice(0, -1), pf)
    const data = res.data?.data
    const reply = data?.reply || ''
    const toolCalls = data?.tool_calls as ChatToolCall[] | undefined

    // Execute tool calls if present
    if (toolCalls && toolCalls.length > 0) {
      const actions: string[] = []
      for (const tc of toolCalls) {
        try {
          const args = JSON.parse(tc.function.arguments)
          if (tc.function.name === 'remove_file') {
            const path = args.path as string
            const before = previewFiles.value.length
            previewFiles.value = previewFiles.value.filter(f => f.path !== path)
            expandedFiles.value.delete(path)
            expandedFiles.value = new Set(expandedFiles.value)
            if (previewFiles.value.length < before) {
              actions.push(`Removed "${path}"`)
            }
          } else if (tc.function.name === 'add_file') {
            const path = args.path as string
            const content = args.content as string
            if (!previewFiles.value.find(f => f.path === path)) {
              previewFiles.value.push({
                path,
                old_content: '',
                new_content: content,
                is_new: true,
              })
              expandedFiles.value.add(path)
              expandedFiles.value = new Set(expandedFiles.value)
              actions.push(`Added "${path}"`)
            }
          } else if (tc.function.name === 'update_file_content') {
            const path = args.path as string
            const content = args.content as string
            const file = previewFiles.value.find(f => f.path === path)
            if (file) {
              file.new_content = content
              actions.push(`Updated "${path}"`)
            }
          }
        } catch { /* skip malformed tool call */ }
      }
      if (previewFiles.value.length === 0) {
        previewMode.value = false
        actions.push('All files removed — nothing to submit.')
      }
      // Show reply + action summary
      const actionSummary = actions.length > 0 ? '\n\n' + actions.join('. ') + '.' : ''
      messages.value.push({ role: 'assistant', content: (reply || 'Done.') + actionSummary })
    } else {
      messages.value.push({ role: 'assistant', content: reply || 'No response' })
    }
  } catch (e: any) {
    const msg = e.response?.data?.message || 'Request failed'
    error.value = msg
    // Keep the user's message but mark it as failed, restore input for retry
    const lastMsg = messages.value[messages.value.length - 1]
    if (lastMsg?.role === 'user') {
      input.value = lastMsg.content
      messages.value.pop()
    }
  } finally {
    loading.value = false
    await scrollToBottom()
  }
}

async function scrollToBottom() {
  await nextTick()
  if (messagesEl.value) {
    messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}

defineExpose({ startOptimizePreview, toggle, open })
</script>

<template>
  <!-- Floating button -->
  <button
    v-if="!open"
    class="fixed bottom-6 right-6 z-40 flex h-12 w-12 items-center justify-center rounded-full bg-indigo-600 text-white shadow-lg hover:bg-indigo-700 transition-colors"
    title="Chat with AI"
    @click="toggle"
  >
    <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
    </svg>
  </button>

  <!-- Chat drawer (wider when preview is active) -->
  <div
    v-if="open"
    class="fixed top-0 right-0 z-50 flex h-full flex-col border-l border-gray-200 bg-white shadow-xl transition-all"
    :class="previewMode && previewFiles.length > 0 ? 'w-[720px]' : 'w-[400px]'"
  >
    <!-- Header -->
    <div class="flex items-center justify-between border-b border-gray-200 px-4 py-3">
      <h3 class="text-sm font-semibold text-gray-900">
        {{ previewMode ? 'Optimize Preview' : 'Repo Chat' }}
      </h3>
      <div class="flex items-center space-x-2">
        <button
          v-if="previewMode && !confirmLoading"
          class="text-xs text-gray-500 hover:text-gray-700"
          @click="previewMode = false; previewFiles = []"
        >Cancel</button>
        <button class="text-gray-400 hover:text-gray-600" @click="toggle">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
          </svg>
        </button>
      </div>
    </div>

    <!-- File diffs (fixed at top, own scroll area) -->
    <div v-if="previewMode && previewFiles.length > 0" class="shrink-0 max-h-[50%] overflow-y-auto border-b border-gray-200 px-4 py-3 space-y-3">
      <div v-for="file in previewFiles" :key="file.path" class="rounded-md border border-gray-200 overflow-hidden">
        <div class="flex items-center justify-between bg-gray-50 px-3 py-2 text-sm">
          <button
            class="flex items-center space-x-2 text-left hover:bg-gray-100 flex-1 min-w-0"
            @click="toggleFile(file.path)"
          >
            <span class="font-mono text-xs text-gray-700 truncate">{{ file.path }}</span>
            <span
              class="rounded px-1.5 py-0.5 text-xs shrink-0"
              :class="file.is_new ? 'bg-green-100 text-green-700' : 'bg-yellow-100 text-yellow-700'"
            >{{ file.is_new ? 'NEW' : 'MODIFIED' }}</span>
            <svg
              class="h-4 w-4 text-gray-400 transition-transform shrink-0"
              :class="{ 'rotate-180': expandedFiles.has(file.path) }"
              xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor"
            >
              <path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd" />
            </svg>
          </button>
          <button
            class="ml-2 shrink-0 rounded px-1.5 py-0.5 text-xs text-red-600 hover:bg-red-50 hover:text-red-800"
            title="Remove from PR"
            @click.stop="removeFile(file.path)"
          >Remove</button>
        </div>
        <div v-if="expandedFiles.has(file.path)" class="max-h-[300px] overflow-auto">
          <CodeDiff
            :old-string="file.old_content"
            :new-string="file.new_content"
            :filename="file.path"
            output-format="line-by-line"
            :context="3"
          />
        </div>
      </div>
      <!-- Submit PR button inside diff area -->
      <div class="flex justify-end">
        <button
          class="rounded-md bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-700 disabled:opacity-50"
          :disabled="confirmLoading"
          @click="handleConfirm"
        >{{ confirmLoading ? 'Creating PR...' : 'Submit PR' }}</button>
      </div>
    </div>

    <!-- Chat messages area -->
    <div ref="messagesEl" class="flex-1 min-h-0 overflow-y-auto px-4 py-3 space-y-3">
      <div v-if="messages.length === 0 && !previewMode" class="text-center text-sm text-gray-400 mt-8">
        Ask questions about this repository's code and scan results.
      </div>
      <div
        v-for="(msg, i) in messages"
        :key="i"
        class="flex"
        :class="msg.role === 'user' ? 'justify-end' : 'justify-start'"
      >
        <div
          class="max-w-[85%] rounded-lg px-3 py-2 text-sm whitespace-pre-wrap"
          :class="msg.role === 'user' ? 'bg-indigo-600 text-white' : 'bg-gray-100 text-gray-900'"
        >
          {{ msg.content }}
        </div>
      </div>

      <!-- Preview loading -->
      <div v-if="previewLoading" class="flex justify-start">
        <div class="rounded-lg bg-gray-100 px-3 py-2 text-sm text-gray-500">Generating preview...</div>
      </div>

      <!-- Chat loading -->
      <div v-if="loading" class="flex justify-start">
        <div class="rounded-lg bg-gray-100 px-3 py-2 text-sm text-gray-500">Thinking...</div>
      </div>
    </div>

    <!-- PR result (shown even after previewMode is off) -->
    <div v-if="confirmResult?.pr_url" class="border-t border-gray-200 px-4 py-3">
      <div class="rounded-md bg-green-50 p-3 text-sm text-green-700">
        PR created successfully!
        <a :href="confirmResult.pr_url" target="_blank" class="font-medium underline">View PR &rarr;</a>
      </div>
    </div>

    <!-- Error -->
    <div v-if="error" class="px-4 py-2 text-xs text-red-600 bg-red-50">{{ error }}</div>

    <!-- Input -->
    <div class="border-t border-gray-200 px-4 py-3">
      <div class="flex space-x-2">
        <textarea
          v-model="input"
          rows="2"
          class="flex-1 resize-none rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          :placeholder="previewMode ? 'Ask about the proposed changes...' : 'Ask about this repo...'"
          @keydown="handleKeydown"
        />
        <button
          class="self-end rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50"
          :disabled="!input.trim() || loading"
          @click="send"
        >
          Send
        </button>
      </div>
    </div>
  </div>
</template>
