package dal

import (
 "strings"
 "project/internal/model"
 "project/pkg/global"
 "gorm.io/gorm"
 "gorm.io/gorm/clause"
)

// This is the explicit staged legacy-owner implementation. Mainline instead
// participates in its established device/configuration lock ordering.
func updateDeviceConfigMetadataOwner(id, tenantID string, values map[string]interface{}, revision *int, result *DeviceConfigMutation) error {
 if strings.TrimSpace(tenantID)=="" {return gorm.ErrRecordNotFound}
 if _,ok:=values["id"];ok {return ErrDeviceConfigBindingConflict}
 if _,ok:=values["tenant_id"];ok {return ErrDeviceConfigBindingConflict}
 return global.DB.Transaction(func(tx *gorm.DB) error {
  var config model.DeviceConfig
  if err:=tx.Clauses(clause.Locking{Strength:"UPDATE"}).Where("id = ? AND tenant_id = ?",id,tenantID).First(&config).Error;err!=nil {return err}
  if err:=prepareDeviceConfigMetadata(&config,values,result);err!=nil {return err}
  if revision!=nil && config.NativeBindingRevision!=*revision {return ErrNativeBindingRevisionConflict}
  updated:=tx.Model(&model.DeviceConfig{}).Where("id = ? AND tenant_id = ?",id,tenantID).Updates(values)
  if updated.Error!=nil {return updated.Error}
  if updated.RowsAffected!=1 {return gorm.ErrRecordNotFound}
  var final model.DeviceConfig
  if err:=tx.Where("id = ? AND tenant_id = ?",id,tenantID).First(&final).Error;err!=nil{return err}
  result.After=&final
  return nil
 })
}
