/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { z } from 'zod'

// Tenant status values, matching common.TenantStatusEnabled/Disabled on the backend.
export const TENANT_STATUS_ENABLED = 1
export const TENANT_STATUS_DISABLED = 2

export const tenantSchema = z.object({
  id: z.number(),
  name: z.string(),
  status: z.number(),
  remark: z.string().optional(),
  created_time: z.number().optional(),
  updated_time: z.number().optional(),
})
export type Tenant = z.infer<typeof tenantSchema>

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetTenantsResponse {
  success: boolean
  message?: string
  data?: {
    items: Tenant[]
    total: number
    page: number
    page_size: number
  }
}

export interface TenantFormData {
  id?: number
  name: string
  status?: number
  remark?: string
}
