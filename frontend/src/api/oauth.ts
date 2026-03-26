import client from './client'

export interface ApproveRequest {
  client_id: string
  redirect_uri: string
  code_challenge: string
  code_challenge_method: string
  state: string
  approved: boolean
}

export interface ApproveResponse {
  redirect_uri: string
}

export async function approveAuthorization(req: ApproveRequest): Promise<ApproveResponse> {
  const { data } = await client.post('/oauth/authorize/approve', req)
  return data.data
}
