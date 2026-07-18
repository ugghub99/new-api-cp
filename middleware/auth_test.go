package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newTestGinContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return c
}

func TestEffectiveTenantId(t *testing.T) {
	t.Run("session/dashboard tenant_id key", func(t *testing.T) {
		c := newTestGinContext()
		c.Set("tenant_id", 5)
		assert.Equal(t, 5, EffectiveTenantId(c))
	})

	t.Run("token/relay ContextKeyUserTenantId falls back when tenant_id unset", func(t *testing.T) {
		c := newTestGinContext()
		common.SetContextKey(c, constant.ContextKeyUserTenantId, 7)
		assert.Equal(t, 7, EffectiveTenantId(c))
	})

	t.Run("session tenant_id takes priority when both set", func(t *testing.T) {
		c := newTestGinContext()
		c.Set("tenant_id", 5)
		common.SetContextKey(c, constant.ContextKeyUserTenantId, 7)
		assert.Equal(t, 5, EffectiveTenantId(c))
	})

	t.Run("explicit zero tenant_id (shared) is returned, not treated as unset", func(t *testing.T) {
		c := newTestGinContext()
		c.Set("tenant_id", 0)
		assert.Equal(t, 0, EffectiveTenantId(c))
	})

	t.Run("nothing set defaults to shared (0)", func(t *testing.T) {
		c := newTestGinContext()
		assert.Equal(t, 0, EffectiveTenantId(c))
	})
}

func TestAssertSameTenant(t *testing.T) {
	tests := []struct {
		name             string
		role             int
		callerTenantId   int
		resourceTenantId int
		allowShared      bool
		want             bool
	}{
		{"root bypasses even cross-tenant", common.RoleRootUser, 5, 99, false, true},
		{"admin same tenant allowed", common.RoleAdminUser, 5, 5, false, true},
		{"admin different tenant denied", common.RoleAdminUser, 5, 7, false, false},
		{"admin shared resource denied without allowShared", common.RoleAdminUser, 5, 0, false, false},
		{"admin shared resource allowed with allowShared", common.RoleAdminUser, 5, 0, true, true},
		{"admin own tenant allowed regardless of allowShared", common.RoleAdminUser, 5, 5, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestGinContext()
			c.Set("role", tt.role)
			c.Set("tenant_id", tt.callerTenantId)
			assert.Equal(t, tt.want, AssertSameTenant(c, tt.resourceTenantId, tt.allowShared))
		})
	}
}
