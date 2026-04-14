import client from './client'
import type { ApiResponse } from '@/types'

export interface LLMConfig {
  sub2api_url: string
  sub2api_api_key: string
  relay_url?: string
  relay_api_key?: string
  relay_admin_api_key?: string
  enabled?: boolean
  model: string
  max_tokens_per_scan?: number
  max_scans_per_repo_per_day?: number
  system_prompt?: string
  user_prompt_template?: string
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
