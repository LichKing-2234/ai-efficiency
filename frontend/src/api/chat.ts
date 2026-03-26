import client from './client'

export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
}

export interface ChatToolCall {
  id: string
  type: string
  function: {
    name: string
    arguments: string
  }
}

export interface PreviewFileContext {
  path: string
  new_content: string
}

export interface ChatResponse {
  reply: string
  tokens_used: number
  tool_calls?: ChatToolCall[]
}

export function sendChatMessage(repoId: number, message: string, history: ChatMessage[], previewFiles?: PreviewFileContext[]) {
  return client.post<{ code: number; data: ChatResponse }>(`/repos/${repoId}/chat`, {
    message,
    history,
    preview_files: previewFiles,
  }, { timeout: 120000 })
}
