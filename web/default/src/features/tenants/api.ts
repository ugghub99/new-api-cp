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
import { api } from '@/lib/api'

import type { ApiResponse, GetTenantsResponse, Tenant, TenantFormData } from './types'

/**
 * Get paginated tenants list (Root only; the backend also enforces this).
 */
export async function getTenants(
  params: { p?: number; page_size?: number } = {}
): Promise<GetTenantsResponse> {
  const { p = 1, page_size = 100 } = params
  const res = await api.get(`/api/tenant/?p=${p}&page_size=${page_size}`)
  return res.data
}

export async function createTenant(
  data: TenantFormData
): Promise<ApiResponse<Tenant>> {
  const res = await api.post('/api/tenant/', data)
  return res.data
}

export async function updateTenant(
  data: TenantFormData & { id: number }
): Promise<ApiResponse<Tenant>> {
  const res = await api.put('/api/tenant/', data)
  return res.data
}

export async function deleteTenant(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/tenant/${id}`)
  return res.data
}
