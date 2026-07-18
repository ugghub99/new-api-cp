package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDistributorTestDB(t *testing.T) {
	t.Helper()

	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func insertDistributorTestChannel(t *testing.T, id int, tenantId int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:       id,
		Type:     1,
		Key:      "key",
		Status:   common.ChannelStatusEnabled,
		Name:     "channel",
		Group:    "default",
		Models:   "gpt-4",
		TenantId: tenantId,
	}).Error)
}

func newDistributorTestContext(t *testing.T, channelId int, callerTenantId int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleCommonUser)
	c.Set("tenant_id", callerTenantId)
	common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, strconv.Itoa(channelId))
	return c, recorder
}

func TestDistribute_ExplicitChannelId_CrossTenantForbidden(t *testing.T) {
	setupDistributorTestDB(t)
	insertDistributorTestChannel(t, 101, 7) // private to tenant 7

	c, recorder := newDistributorTestContext(t, 101, 9) // caller is tenant 9
	Distribute()(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestDistribute_ExplicitChannelId_SameTenantAllowed(t *testing.T) {
	setupDistributorTestDB(t)
	insertDistributorTestChannel(t, 102, 7) // private to tenant 7

	c, recorder := newDistributorTestContext(t, 102, 7) // caller is the owning tenant
	Distribute()(c)

	require.NotEqual(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, 102, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
}

func TestDistribute_ExplicitChannelId_SharedChannelAllowedForAnyTenant(t *testing.T) {
	setupDistributorTestDB(t)
	insertDistributorTestChannel(t, 103, 0) // shared

	c, recorder := newDistributorTestContext(t, 103, 42)
	Distribute()(c)

	require.NotEqual(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, 103, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
}
