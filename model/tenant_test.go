package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetTenantTestTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Tenant{}, &User{}, &Channel{}))
	for _, table := range []string{"tenants", "users", "channels"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range []string{"tenants", "users", "channels"} {
			require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
		}
	})
}

func TestTenant_InsertAndGet(t *testing.T) {
	resetTenantTestTables(t)

	tenant := &Tenant{Name: "acme"}
	require.NoError(t, tenant.Insert())
	require.NotZero(t, tenant.Id)

	fetched, err := GetTenantById(tenant.Id)
	require.NoError(t, err)
	assert.Equal(t, "acme", fetched.Name)
}

func TestGetTenantById_ZeroIsRejected(t *testing.T) {
	resetTenantTestTables(t)
	_, err := GetTenantById(0)
	assert.Error(t, err, "tenant_id 0 is the shared/global sentinel, not a real tenant row")
}

func TestIsTenantNameDuplicated(t *testing.T) {
	resetTenantTestTables(t)
	tenant := &Tenant{Name: "acme"}
	require.NoError(t, tenant.Insert())

	dup, err := IsTenantNameDuplicated(0, "acme")
	require.NoError(t, err)
	assert.True(t, dup)

	dup, err = IsTenantNameDuplicated(tenant.Id, "acme")
	require.NoError(t, err)
	assert.False(t, dup, "a tenant should not conflict with its own name")

	dup, err = IsTenantNameDuplicated(0, "globex")
	require.NoError(t, err)
	assert.False(t, dup)
}

func TestDeleteTenantById_BlockedByReferences(t *testing.T) {
	resetTenantTestTables(t)
	tenant := &Tenant{Name: "acme"}
	require.NoError(t, tenant.Insert())

	t.Run("blocked while a user references it", func(t *testing.T) {
		require.NoError(t, DB.Create(&User{Username: "u1", Password: "password12", TenantId: tenant.Id, AffCode: "aff-1"}).Error)
		assert.Error(t, DeleteTenantById(tenant.Id))
		require.NoError(t, DB.Exec("DELETE FROM users WHERE tenant_id = ?", tenant.Id).Error)
	})

	t.Run("blocked while a channel references it", func(t *testing.T) {
		require.NoError(t, DB.Create(&Channel{Type: 1, Key: "k", Name: "c1", TenantId: tenant.Id}).Error)
		assert.Error(t, DeleteTenantById(tenant.Id))
		require.NoError(t, DB.Exec("DELETE FROM channels WHERE tenant_id = ?", tenant.Id).Error)
	})

	t.Run("succeeds once no references remain", func(t *testing.T) {
		require.NoError(t, DeleteTenantById(tenant.Id))
		_, err := GetTenantById(tenant.Id)
		assert.Error(t, err)
	})

	assert.Error(t, DeleteTenantById(0), "the shared/global sentinel must never be deletable")
}
