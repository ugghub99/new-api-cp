package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetTenants 分页获取租户列表（仅 Root 可访问）
func GetTenants(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	tenants, total, err := model.GetAllTenants(pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tenants)
	common.ApiSuccess(c, pageInfo)
}

// CreateTenant 创建新租户（仅 Root 可访问）
func CreateTenant(c *gin.Context) {
	var t model.Tenant
	if err := c.ShouldBindJSON(&t); err != nil {
		common.ApiError(c, err)
		return
	}
	if t.Name == "" {
		common.ApiErrorMsg(c, "租户名称不能为空")
		return
	}
	if dup, err := model.IsTenantNameDuplicated(0, t.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "租户名称已存在")
		return
	}
	if err := t.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &t)
}

// UpdateTenant 更新租户（仅 Root 可访问）
func UpdateTenant(c *gin.Context) {
	var t model.Tenant
	if err := c.ShouldBindJSON(&t); err != nil {
		common.ApiError(c, err)
		return
	}
	if t.Id == 0 {
		common.ApiErrorMsg(c, "缺少租户 ID")
		return
	}
	if dup, err := model.IsTenantNameDuplicated(t.Id, t.Name); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "租户名称已存在")
		return
	}
	if err := t.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &t)
}

// DeleteTenant 删除租户（仅 Root 可访问）。若仍有用户或渠道归属该租户则拒绝删除。
func DeleteTenant(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteTenantById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
