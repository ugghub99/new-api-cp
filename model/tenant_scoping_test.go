package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetTenantScopingTestTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{"users", "channels", "logs"} {
		require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
	}
	t.Cleanup(func() {
		for _, table := range []string{"users", "channels", "logs"} {
			require.NoError(t, DB.Exec("DELETE FROM "+table).Error)
		}
	})
}

func TestGetAllUsers_TenantFilter(t *testing.T) {
	resetTenantScopingTestTables(t)
	require.NoError(t, DB.Create(&User{Username: "u-shared", Password: "password12", TenantId: 0, AffCode: "aff-shared"}).Error)
	require.NoError(t, DB.Create(&User{Username: "u-tenant7", Password: "password12", TenantId: 7, AffCode: "aff-t7"}).Error)
	require.NoError(t, DB.Create(&User{Username: "u-tenant9", Password: "password12", TenantId: 9, AffCode: "aff-t9"}).Error)

	t.Run("nil tenantId returns every user (root, no filter)", func(t *testing.T) {
		users, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 10}, nil)
		require.NoError(t, err)
		assert.EqualValues(t, 3, total)
		assert.Len(t, users, 3)
	})

	t.Run("non-nil tenantId restricts to an exact match", func(t *testing.T) {
		tenantId := 7
		users, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 10}, &tenantId)
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		require.Len(t, users, 1)
		assert.Equal(t, "u-tenant7", users[0].Username)
	})

	t.Run("tenantId 0 is a meaningful filter, not \"no filter\"", func(t *testing.T) {
		tenantId := 0
		users, total, err := GetAllUsers(&common.PageInfo{Page: 1, PageSize: 10}, &tenantId)
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		assert.Equal(t, "u-shared", users[0].Username)
	})
}

func TestSearchUsers_TenantFilter(t *testing.T) {
	resetTenantScopingTestTables(t)
	require.NoError(t, DB.Create(&User{Username: "search-tenant7", Password: "password12", TenantId: 7, AffCode: "search-aff-t7"}).Error)
	require.NoError(t, DB.Create(&User{Username: "search-tenant9", Password: "password12", TenantId: 9, AffCode: "search-aff-t9"}).Error)

	tenantId := 7
	users, total, err := SearchUsers("search", "", nil, nil, &tenantId, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Equal(t, "search-tenant7", users[0].Username)
}

func insertTenantScopingChannel(t *testing.T, id int, tenantId int, name string) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{
		Id:       id,
		Type:     1,
		Key:      "key",
		Status:   common.ChannelStatusEnabled,
		Name:     name,
		Group:    "default",
		Models:   "gpt-4",
		TenantId: tenantId,
	}).Error)
}

func TestSearchChannels_TenantFilter(t *testing.T) {
	resetTenantScopingTestTables(t)
	insertTenantScopingChannel(t, 201, 0, "shared-channel")
	insertTenantScopingChannel(t, 202, 7, "tenant7-channel")
	insertTenantScopingChannel(t, 203, 9, "tenant9-channel")

	t.Run("nil tenantId sees every channel", func(t *testing.T) {
		channels, err := SearchChannels("channel", "", "", false, nil)
		require.NoError(t, err)
		assert.Len(t, channels, 3)
	})

	t.Run("tenant 7 sees shared plus its own, never tenant 9's", func(t *testing.T) {
		tenantId := 7
		channels, err := SearchChannels("channel", "", "", false, &tenantId)
		require.NoError(t, err)
		ids := make([]int, 0, len(channels))
		for _, ch := range channels {
			ids = append(ids, ch.Id)
		}
		assert.ElementsMatch(t, []int{201, 202}, ids)
	})
}

func TestApplyChannelTenantFilter(t *testing.T) {
	resetTenantScopingTestTables(t)
	insertTenantScopingChannel(t, 211, 0, "shared")
	insertTenantScopingChannel(t, 212, 5, "tenant5")
	insertTenantScopingChannel(t, 213, 6, "tenant6")

	tenantId := 5
	var channels []*Channel
	require.NoError(t, ApplyChannelTenantFilter(DB.Model(&Channel{}), &tenantId).Find(&channels).Error)
	ids := make([]int, 0, len(channels))
	for _, ch := range channels {
		ids = append(ids, ch.Id)
	}
	assert.ElementsMatch(t, []int{211, 212}, ids)
}

func TestGetAllLogs_TenantFilter(t *testing.T) {
	resetTenantScopingTestTables(t)
	require.NoError(t, DB.Create(&Log{UserId: 1, TenantId: 0, Type: LogTypeConsume, CreatedAt: 1}).Error)
	require.NoError(t, DB.Create(&Log{UserId: 2, TenantId: 7, Type: LogTypeConsume, CreatedAt: 2}).Error)
	require.NoError(t, DB.Create(&Log{UserId: 3, TenantId: 9, Type: LogTypeConsume, CreatedAt: 3}).Error)

	t.Run("tenantId 0 means no filter (root, all tenants)", func(t *testing.T) {
		logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "", 0)
		require.NoError(t, err)
		assert.EqualValues(t, 3, total)
		assert.Len(t, logs, 3)
	})

	t.Run("non-zero tenantId restricts to that tenant only", func(t *testing.T) {
		logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 10, 0, "", "", "", 7)
		require.NoError(t, err)
		require.EqualValues(t, 1, total)
		require.Len(t, logs, 1)
		assert.Equal(t, 2, logs[0].UserId)
	})
}
