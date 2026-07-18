package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupTenantControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Tenant{}))
}

type tenantResponse struct {
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Data    model.Tenant `json:"data"`
}

func TestTenantCRUD_RootRoundTrip(t *testing.T) {
	setupTenantControllerTestDB(t)

	// Create
	createRecorder := httptest.NewRecorder()
	createCtx, _ := gin.CreateTestContext(createRecorder)
	createCtx.Request = httptest.NewRequest(http.MethodPost, "/api/tenant/", bytes.NewBufferString(`{"name":"acme"}`))
	createCtx.Request.Header.Set("Content-Type", "application/json")
	CreateTenant(createCtx)

	require.Equal(t, http.StatusOK, createRecorder.Code)
	var created tenantResponse
	require.NoError(t, common.Unmarshal(createRecorder.Body.Bytes(), &created))
	require.True(t, created.Success, createRecorder.Body.String())
	require.NotZero(t, created.Data.Id)

	// Duplicate name is rejected
	dupRecorder := httptest.NewRecorder()
	dupCtx, _ := gin.CreateTestContext(dupRecorder)
	dupCtx.Request = httptest.NewRequest(http.MethodPost, "/api/tenant/", bytes.NewBufferString(`{"name":"acme"}`))
	dupCtx.Request.Header.Set("Content-Type", "application/json")
	CreateTenant(dupCtx)
	var dupResp tenantResponse
	require.NoError(t, common.Unmarshal(dupRecorder.Body.Bytes(), &dupResp))
	require.False(t, dupResp.Success)

	// Update
	updateRecorder := httptest.NewRecorder()
	updateCtx, _ := gin.CreateTestContext(updateRecorder)
	updateBody := `{"id":` + strconv.Itoa(created.Data.Id) + `,"name":"acme-renamed"}`
	updateCtx.Request = httptest.NewRequest(http.MethodPut, "/api/tenant/", bytes.NewBufferString(updateBody))
	updateCtx.Request.Header.Set("Content-Type", "application/json")
	UpdateTenant(updateCtx)
	var updated tenantResponse
	require.NoError(t, common.Unmarshal(updateRecorder.Body.Bytes(), &updated))
	require.True(t, updated.Success, updateRecorder.Body.String())
	require.Equal(t, "acme-renamed", updated.Data.Name)

	// List
	listRecorder := httptest.NewRecorder()
	listCtx, _ := gin.CreateTestContext(listRecorder)
	listCtx.Request = httptest.NewRequest(http.MethodGet, "/api/tenant/?p=1&page_size=10", nil)
	GetTenants(listCtx)
	require.Equal(t, http.StatusOK, listRecorder.Code)

	// Delete (no references) succeeds
	deleteRecorder := httptest.NewRecorder()
	deleteCtx, _ := gin.CreateTestContext(deleteRecorder)
	deleteCtx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(created.Data.Id)}}
	DeleteTenant(deleteCtx)
	var deleted tenantResponse
	require.NoError(t, common.Unmarshal(deleteRecorder.Body.Bytes(), &deleted))
	require.True(t, deleted.Success, deleteRecorder.Body.String())
}

func TestDeleteTenant_BlockedByReferencesReturnsError(t *testing.T) {
	setupTenantControllerTestDB(t)

	tenant := &model.Tenant{Name: "referenced-tenant"}
	require.NoError(t, tenant.Insert())
	require.NoError(t, model.DB.Create(&model.User{Username: "referencing-user", Password: "password12", TenantId: tenant.Id, AffCode: "aff-ref"}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(tenant.Id)}}
	DeleteTenant(ctx)

	var resp tenantResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.False(t, resp.Success, "deleting a tenant with a referencing user must fail")
}
