package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetAbilityTenantTestTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))
	for _, table := range []string{"abilities", "channels"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range []string{"abilities", "channels"} {
			require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
		}
	})
}

func insertTenantChannel(t *testing.T, id int, tenantId int) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{
		Id:       id,
		Type:     1,
		Key:      "key",
		Status:   common.ChannelStatusEnabled,
		Name:     "channel",
		Group:    "default",
		Models:   "gpt-4",
		TenantId: tenantId,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4",
		ChannelId: id,
		TenantId:  tenantId,
		Enabled:   true,
	}).Error)
}

func TestGetChannel_TenantIsolation(t *testing.T) {
	resetAbilityTenantTestTables(t)

	t.Run("private channel only reachable by its own tenant", func(t *testing.T) {
		resetAbilityTenantTestTables(t)
		insertTenantChannel(t, 1, 7)

		ch, err := GetChannel("default", "gpt-4", 0, "", 7)
		require.NoError(t, err)
		require.NotNil(t, ch)
		assert.Equal(t, 1, ch.Id)

		ch, err = GetChannel("default", "gpt-4", 0, "", 9)
		require.NoError(t, err)
		assert.Nil(t, ch)

		ch, err = GetChannel("default", "gpt-4", 0, "", 0)
		require.NoError(t, err)
		assert.Nil(t, ch)
	})

	t.Run("shared channel reachable by every tenant", func(t *testing.T) {
		resetAbilityTenantTestTables(t)
		insertTenantChannel(t, 2, 0)

		for _, tenantId := range []int{0, 5, 9} {
			ch, err := GetChannel("default", "gpt-4", 0, "", tenantId)
			require.NoError(t, err)
			require.NotNil(t, ch)
			assert.Equal(t, 2, ch.Id)
		}
	})

	t.Run("foreign tenant never sees another tenant's private channel even with a shared one present", func(t *testing.T) {
		resetAbilityTenantTestTables(t)
		insertTenantChannel(t, 3, 0) // shared
		insertTenantChannel(t, 4, 7) // private to tenant 7

		for i := 0; i < 20; i++ {
			ch, err := GetChannel("default", "gpt-4", 0, "", 9)
			require.NoError(t, err)
			require.NotNil(t, ch)
			assert.Equal(t, 3, ch.Id, "tenant 9 must only ever see the shared channel")
		}

		seenPrivate := false
		for i := 0; i < 20; i++ {
			ch, err := GetChannel("default", "gpt-4", 0, "", 7)
			require.NoError(t, err)
			require.NotNil(t, ch)
			assert.Contains(t, []int{3, 4}, ch.Id)
			if ch.Id == 4 {
				seenPrivate = true
			}
		}
		assert.True(t, seenPrivate, "tenant 7 should be able to reach its own private channel")
	})
}
