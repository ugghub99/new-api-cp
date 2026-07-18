package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestCreateUser_ForceAssignsOwnTenant is a security-relevant test: a
// tenant-scoped Admin must never be able to create a user in a different
// tenant by claiming one in the request body, even though tenant_id is a
// bindable field on model.User.
func TestCreateUser_ForceAssignsOwnTenant(t *testing.T) {
	require.NoError(t, i18n.Init())
	setupModelListControllerTestDB(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/",
		bytes.NewBufferString(`{"username":"tenantscopeduser","password":"password123","role":1,"tenant_id":999}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	// Caller is a tenant-scoped Admin (not Root) belonging to tenant 7.
	c.Set("role", common.RoleAdminUser)
	c.Set("id", 1)
	c.Set("tenant_id", 7)

	CreateUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())

	var created model.User
	require.NoError(t, model.DB.Where("username = ?", "tenantscopeduser").First(&created).Error)
	require.Equal(t, 7, created.TenantId, "tenant-scoped Admin must be force-assigned their own tenant, not the claimed 999")
}

// TestCreateUser_RootMayAssignAnyTenant confirms Root is not subject to the
// same force-assignment and may set an arbitrary tenant on creation.
func TestCreateUser_RootMayAssignAnyTenant(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/user/",
		bytes.NewBufferString(`{"username":"rootcreateduser","password":"password123","role":1,"tenant_id":42}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleRootUser)
	c.Set("id", 1)
	c.Set("tenant_id", 0)

	CreateUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var created model.User
	require.NoError(t, model.DB.Where("username = ?", "rootcreateduser").First(&created).Error)
	require.Equal(t, 42, created.TenantId)
}

// TestUpdateUser_RootReassignsTenant_ActuallyPersists is a regression test
// for a bug caught by live verification (not by any prior unit test): the
// tenant_id captured for updateUserTenantForUserInTx must be read BEFORE
// EditWithTx's trailing refetch (tx.First(user, user.Id)) overwrites the
// in-memory updatedUser struct with the pre-update DB row. Reading
// updatedUser.TenantId after that refetch silently observes the OLD value,
// making the reassignment a no-op while still reporting success=true.
func TestUpdateUser_RootReassignsTenant_ActuallyPersists(t *testing.T) {
	require.NoError(t, i18n.Init())
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}))

	// Role is Admin (not Common/Guest) so updateAdminPermissionsForUserInTx's
	// nil-permissions branch takes the no-op path instead of the
	// ClearUserAuthorizationInTx path, which needs a fully-initialized authz
	// enforcer this lightweight test fixture doesn't set up — irrelevant to
	// what this test is actually regression-testing (tenant persistence).
	existing := model.User{
		Username:    "reassignme",
		Password:    "irrelevant-hash",
		DisplayName: "Reassign Me",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		TenantId:    0,
	}
	require.NoError(t, model.DB.Create(&existing).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"id":` + strconv.Itoa(existing.Id) + `,"username":"reassignme","role":` + strconv.Itoa(common.RoleAdminUser) + `,"tenant_id":5}`
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleRootUser)
	c.Set("id", 1)
	c.Set("tenant_id", 0)

	UpdateUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())

	var updated model.User
	require.NoError(t, model.DB.First(&updated, existing.Id).Error)
	require.Equal(t, 5, updated.TenantId, "Root's tenant reassignment must actually persist, not just report success")
}
