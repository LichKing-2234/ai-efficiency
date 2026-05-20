import client from './client'
import type { ApiResponse } from '@/types'

export interface LLMConfig {
  relay_url?: string
  relay_api_key?: string
  relay_admin_api_key?: string
  enabled?: boolean
  model: string
}

export interface LLMTestResult {
  success: boolean
  message: string
  response?: string
}

export interface LLMTestRequest {
  prompt?: string
}

export function getLLMConfig() {
  return client.get<ApiResponse<LLMConfig>>('/settings/llm')
}

export function updateLLMConfig(data: Partial<LLMConfig>) {
  return client.put<ApiResponse<LLMConfig>>('/settings/llm', data)
}

export function testLLMConnection(data?: LLMTestRequest) {
  return client.post<ApiResponse<LLMTestResult>>('/settings/llm/test', data)
}
