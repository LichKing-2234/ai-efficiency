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
  const token = localStorage.getItem('token')
  const resp = await fetch('/oauth/authorize/approve', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(req),
  })
  const data = await resp.json()
  if (!resp.ok) {
    throw new Error(data?.message || data?.error || 'Authorization failed')
  }
  return data.data ?? data
}

export interface DeviceVerifyRequest {
  user_code: string
  approved: boolean
}

export interface DeviceVerifyResponse {
  status: 'approved' | 'denied'
}

export async function verifyDeviceAuthorization(req: DeviceVerifyRequest): Promise<DeviceVerifyResponse> {
  const token = localStorage.getItem('token')
  const resp = await fetch('/oauth/device/verify', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(req),
  })
  const data = await resp.json()
  if (!resp.ok) {
    const err: any = new Error(data?.message || data?.error || 'Code invalid or expired')
    err.response = { data }
    throw err
  }
  return data.data ?? data
}
