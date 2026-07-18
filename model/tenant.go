package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Tenant represents an isolated organization. Id == 0 is a reserved
// sentinel ("Shared / Global") and never corresponds to a real row here —
// standard autoincrement across SQLite/MySQL/PostgreSQL starts at 1.
type Tenant struct {
	Id          int            `json:"id"`
	Name        string         `json:"name" gorm:"size:64;not null;uniqueIndex:uk_tenant_name,where:deleted_at IS NULL"`
	Status      int            `json:"status" gorm:"type:int;default:1"`
	Remark      string         `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	CreatedTime int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func (t *Tenant) Insert() error {
	now := common.GetTimestamp()
	t.CreatedTime = now
	t.UpdatedTime = now
	if t.Status == 0 {
		t.Status = common.TenantStatusEnabled
	}
	return DB.Create(t).Error
}

func (t *Tenant) Update() error {
	t.UpdatedTime = common.GetTimestamp()
	return DB.Save(t).Error
}

func IsTenantNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&Tenant{}).Where("name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

func GetTenantById(id int) (*Tenant, error) {
	if id == 0 {
		return nil, errors.New("tenant id 0 is the reserved shared/global sentinel, not a real tenant")
	}
	var tenant Tenant
	err := DB.First(&tenant, "id = ?", id).Error
	return &tenant, err
}

func GetAllTenants(pageInfo *common.PageInfo) (tenants []*Tenant, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&Tenant{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&tenants).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return tenants, total, nil
}

// DeleteTenantById hard-blocks deletion while any User or Channel still
// references this tenant, to avoid orphaning tenant_id foreign keys.
func DeleteTenantById(id int) error {
	if id == 0 {
		return errors.New("tenant id 0 is the reserved shared/global sentinel and cannot be deleted")
	}
	var userCount int64
	if err := DB.Model(&User{}).Where("tenant_id = ?", id).Count(&userCount).Error; err != nil {
		return err
	}
	if userCount > 0 {
		return errors.New("cannot delete tenant: users are still assigned to it")
	}
	var channelCount int64
	if err := DB.Model(&Channel{}).Where("tenant_id = ?", id).Count(&channelCount).Error; err != nil {
		return err
	}
	if channelCount > 0 {
		return errors.New("cannot delete tenant: channels are still assigned to it")
	}
	return DB.Delete(&Tenant{}, id).Error
}
