// src/api/admin.ts
import { api } from './client'

export interface InviteCode {
  id: number
  code: string
  created_by: string
  used_by: string | null
  used_at: string | null
  expires_at: string | null
  created_at: string
}

export interface CreateInviteRequest {
  code?: string
  expires_days?: number
}

export const adminApi = {
  listInvites: () => api.get<InviteCode[]>('/api/v1/admin/invites'),

  createInvite: (req: CreateInviteRequest) =>
    api.post<InviteCode>('/api/v1/admin/invites', req),

  resetInvite: (code: string) =>
    api.post<{ status: string; code: string }>(
      `/api/v1/admin/invites/${code}/reset`,
      {}
    ),
}