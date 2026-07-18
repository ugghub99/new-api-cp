package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRandomSatisfiedChannel_TenantIsolation_MemoryCache(t *testing.T) {
	resetAbilityTenantTestTables(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		InitChannelCache()
	})

	t.Run("private channel only reachable by its own tenant", func(t *testing.T) {
		resetAbilityTenantTestTables(t)
		insertTenantChannel(t, 11, 7)
		InitChannelCache()

		ch, err := GetRandomSatisfiedChannel("default", "gpt-4", 0, "", 7)
		require.NoError(t, err)
		require.NotNil(t, ch)
		assert.Equal(t, 11, ch.Id)

		ch, err = GetRandomSatisfiedChannel("default", "gpt-4", 0, "", 9)
		require.NoError(t, err)
		assert.Nil(t, ch)
	})

	t.Run("foreign tenant never sees another tenant's private channel via memory cache", func(t *testing.T) {
		resetAbilityTenantTestTables(t)
		insertTenantChannel(t, 12, 0) // shared
		insertTenantChannel(t, 13, 7) // private to tenant 7
		InitChannelCache()

		for i := 0; i < 20; i++ {
			ch, err := GetRandomSatisfiedChannel("default", "gpt-4", 0, "", 9)
			require.NoError(t, err)
			require.NotNil(t, ch)
			assert.Equal(t, 12, ch.Id, "tenant 9 must only ever see the shared channel")
		}
	})
}
