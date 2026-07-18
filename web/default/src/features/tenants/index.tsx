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
import { useQuery } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { BadgeCell, StaticDataTable, StaticRowActions } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'

import { deleteTenant, getTenants } from './api'
import { TenantMutateDrawer } from './components/tenant-mutate-drawer'
import { TENANT_STATUS_ENABLED, type Tenant } from './types'

function TenantsContent() {
  const { t } = useTranslation()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [currentRow, setCurrentRow] = useState<Tenant | undefined>()
  const [deleteTarget, setDeleteTarget] = useState<Tenant | undefined>()
  const [isDeleting, setIsDeleting] = useState(false)

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['tenants'],
    queryFn: () => getTenants({ page_size: 100 }),
  })

  const tenants = data?.data?.items ?? []

  const handleCreate = () => {
    setCurrentRow(undefined)
    setDrawerOpen(true)
  }

  const handleEdit = (tenant: Tenant) => {
    setCurrentRow(tenant)
    setDrawerOpen(true)
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setIsDeleting(true)
    try {
      const result = await deleteTenant(deleteTarget.id)
      if (result.success) {
        toast.success(t('Tenant deleted successfully'))
        setDeleteTarget(undefined)
        refetch()
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch {
      toast.error(t('An unexpected error occurred'))
    } finally {
      setIsDeleting(false)
    }
  }

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t('Tenant Management')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button size='sm' onClick={handleCreate}>
            <Plus className='h-4 w-4' />
            {t('Create Tenant')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <StaticDataTable
            data={tenants}
            empty={!isLoading && tenants.length === 0}
            emptyContent={t('No tenants yet')}
            getRowKey={(row) => row.id}
            columns={[
              { id: 'id', header: t('ID'), cell: (row) => row.id },
              { id: 'name', header: t('Name'), cell: (row) => row.name },
              {
                id: 'status',
                header: t('Status'),
                cell: (row) => (
                  <BadgeCell>
                    <StatusBadge
                      variant={
                        row.status === TENANT_STATUS_ENABLED ? 'success' : 'neutral'
                      }
                      label={
                        row.status === TENANT_STATUS_ENABLED
                          ? t('Enabled')
                          : t('Disabled')
                      }
                      copyable={false}
                    />
                  </BadgeCell>
                ),
              },
              {
                id: 'remark',
                header: t('Remark'),
                cell: (row) => row.remark || '-',
              },
              {
                id: 'actions',
                header: '',
                cell: (row) => (
                  <StaticRowActions
                    editLabel={t('Edit')}
                    deleteLabel={t('Delete')}
                    menuLabel={t('Open menu')}
                    onEdit={() => handleEdit(row)}
                    onDelete={() => setDeleteTarget(row)}
                  />
                ),
              },
            ]}
          />
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <TenantMutateDrawer
        open={drawerOpen}
        onOpenChange={setDrawerOpen}
        currentRow={currentRow}
        onSuccess={refetch}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(undefined)}
        title={t('Delete Tenant')}
        desc={t(
          'Are you sure you want to delete "{{name}}"? This cannot be undone. Deletion is blocked while any user or channel still belongs to this tenant.',
          { name: deleteTarget?.name }
        )}
        destructive
        isLoading={isDeleting}
        handleConfirm={handleDelete}
      />
    </>
  )
}

export function Tenants() {
  return <TenantsContent />
}
