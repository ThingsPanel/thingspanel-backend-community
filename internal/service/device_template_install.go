package service

import (
	"context"
	"fmt"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

// InstallFromMarket downloads a template from the market and creates it locally with device models
func (*DeviceTemplate) InstallFromMarket(req model.InstallFromMarketReq, claims *utils.UserClaims) (*model.InstallFromMarketRsp, error) {
	// 1. Download the full template definition from market
	client := NewMarketClient()
	fullData, err := client.DownloadTemplate(context.Background(), req.MarketToken, req.MarketTemplateID, req.Version)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": "Failed to download template from market: " + err.Error(),
		})
	}

	// 2. Check if a template with the same name already exists locally
	existingCount, err := query.DeviceTemplate.
		Where(query.DeviceTemplate.Name.Eq(fullData.Name), query.DeviceTemplate.TenantID.Eq(claims.TenantID)).
		Count()
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to check existing template: " + err.Error(),
		})
	}
	if existingCount > 0 {
		return nil, errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": fmt.Sprintf("Template '%s' already exists locally", fullData.Name),
		})
	}

	// 3. Check plugin dependencies
	missingPlugins := checkMissingPlugins(fullData.PluginDependencies)

	// 4. Use transaction to create template + device models
	now := time.Now().UTC()
	templateID := uuid.New()
	flag := int16(1) // private

	brand := fullData.Brand
	modelNumber := fullData.ModelNumber
	author := fullData.Author
	version := fullData.Version
	description := fullData.Description

	newTemplate := &model.DeviceTemplate{
		ID:          templateID,
		Name:        fullData.Name,
		TenantID:    claims.TenantID,
		Brand:       &brand,
		ModelNumber: &modelNumber,
		Author:      &author,
		Version:     &version,
		Description: &description,
		Flag:        &flag,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Begin transaction
	tx := global.DB.Begin()
	if tx.Error != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to begin transaction: " + tx.Error.Error(),
		})
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Create device template
	if err := tx.Create(newTemplate).Error; err != nil {
		tx.Rollback()
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to create template: " + err.Error(),
		})
	}

	// Create telemetry models
	for _, t := range fullData.Telemetry {
		t.ID = uuid.New()
		t.DeviceTemplateID = templateID
		t.TenantID = claims.TenantID
		t.CreatedAt = now
		t.UpdatedAt = now
		if err := tx.Create(&t).Error; err != nil {
			tx.Rollback()
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "Failed to create telemetry model: " + err.Error(),
			})
		}
	}

	// Create attribute models
	for _, a := range fullData.Attributes {
		a.ID = uuid.New()
		a.DeviceTemplateID = templateID
		a.TenantID = claims.TenantID
		a.CreatedAt = now
		a.UpdatedAt = now
		if err := tx.Create(&a).Error; err != nil {
			tx.Rollback()
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "Failed to create attribute model: " + err.Error(),
			})
		}
	}

	// Create event models
	for _, e := range fullData.Events {
		e.ID = uuid.New()
		e.DeviceTemplateID = templateID
		e.TenantID = claims.TenantID
		e.CreatedAt = now
		e.UpdatedAt = now
		if err := tx.Create(&e).Error; err != nil {
			tx.Rollback()
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "Failed to create event model: " + err.Error(),
			})
		}
	}

	// Create command models
	for _, cmd := range fullData.Commands {
		cmd.ID = uuid.New()
		cmd.DeviceTemplateID = templateID
		cmd.TenantID = claims.TenantID
		cmd.CreatedAt = now
		cmd.UpdatedAt = now
		if err := tx.Create(&cmd).Error; err != nil {
			tx.Rollback()
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": "Failed to create command model: " + err.Error(),
			})
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": "Failed to commit transaction: " + err.Error(),
		})
	}

	// Fetch the created template from DB
	createdTpl, _ := dal.GetDeviceTemplateById(templateID)

	return &model.InstallFromMarketRsp{
		DeviceTemplate: createdTpl,
		MissingPlugins: missingPlugins,
	}, nil
}

// checkMissingPlugins checks which plugin dependencies are not installed locally
func checkMissingPlugins(deps []model.PluginDependency) []model.PluginDependency {
	if len(deps) == 0 {
		return nil
	}

	var missing []model.PluginDependency
	for _, dep := range deps {
		// Check if plugin exists in service_plugins table
		count, err := query.ServicePlugin.
			Where(query.ServicePlugin.ServiceIdentifier.Eq(dep.PluginName)).
			Count()
		if err != nil {
			logrus.Warnf("checkMissingPlugins: failed to query plugin '%s': %v", dep.PluginName, err)
			missing = append(missing, dep)
			continue
		}
		if count == 0 {
			missing = append(missing, dep)
		}
	}
	return missing
}
