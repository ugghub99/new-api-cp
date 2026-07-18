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
import { zodResolver } from '@hookform/resolvers/zod'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'

import { createTenant, updateTenant } from '../api'
import {
  TENANT_STATUS_DISABLED,
  TENANT_STATUS_ENABLED,
  type Tenant,
} from '../types'

const tenantFormSchema = z.object({
  name: z.string().min(1, 'Name is required').max(64),
  status: z.number(),
  remark: z.string().optional(),
})
type TenantFormValues = z.infer<typeof tenantFormSchema>

const DEFAULT_VALUES: TenantFormValues = {
  name: '',
  status: TENANT_STATUS_ENABLED,
  remark: '',
}

type TenantMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Tenant
  onSuccess: () => void
}

export function TenantMutateDrawer({
  open,
  onOpenChange,
  currentRow,
  onSuccess,
}: TenantMutateDrawerProps) {
  const { t } = useTranslation()
  const isUpdate = !!currentRow
  const [isSubmitting, setIsSubmitting] = useState(false)

  const form = useForm<TenantFormValues>({
    resolver: zodResolver(tenantFormSchema),
    values: currentRow
      ? {
          name: currentRow.name,
          status: currentRow.status,
          remark: currentRow.remark || '',
        }
      : DEFAULT_VALUES,
  })

  const onSubmit = async (data: TenantFormValues) => {
    setIsSubmitting(true)
    try {
      const result = isUpdate
        ? await updateTenant({ ...data, id: currentRow.id })
        : await createTenant(data)

      if (result.success) {
        toast.success(
          isUpdate ? t('Tenant updated successfully') : t('Tenant created successfully')
        )
        onOpenChange(false)
        onSuccess()
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch {
      toast.error(t('An unexpected error occurred'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) form.reset()
      }}
    >
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[480px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isUpdate ? t('Edit Tenant') : t('Create Tenant')}
          </SheetTitle>
          <SheetDescription>
            {isUpdate
              ? t('Update this tenant.')
              : t('Add a new tenant organization.')}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='tenant-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            <SideDrawerSection>
              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Name')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder={t('Enter tenant name')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='status'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Status')}</FormLabel>
                    <Select
                      items={[
                        { value: String(TENANT_STATUS_ENABLED), label: t('Enabled') },
                        { value: String(TENANT_STATUS_DISABLED), label: t('Disabled') },
                      ]}
                      onValueChange={(value) =>
                        value !== null && field.onChange(parseInt(value))
                      }
                      value={String(field.value)}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder={t('Select status')} />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value={String(TENANT_STATUS_ENABLED)}>
                            {t('Enabled')}
                          </SelectItem>
                          <SelectItem value={String(TENANT_STATUS_DISABLED)}>
                            {t('Disabled')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='remark'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Remark')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} placeholder={t('Optional remark')} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button form='tenant-form' type='submit' disabled={isSubmitting}>
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
