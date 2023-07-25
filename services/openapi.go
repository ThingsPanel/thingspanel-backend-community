package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"github.com/aliyun/credentials-go/credentials/utils"
	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
	"math/rand"
)

type OpenApiService struct {
}

func (*OpenApiService) GetOpenApiAuthList(validate valid.OpenApiPaginationValidate) (bool, []models.TpOpenapiAuth, int64) {
	openapiAuths := []models.TpOpenapiAuth{}
	offset := (validate.CurrentPage - 1) * validate.PerPage
	db := psql.Mydb.Model(&models.TpOpenapiAuth{})
	if validate.Name != "" {
		db.Where("name like ?", "%"+validate.Name+"%")
	}
	var count int64
	db.Count(&count)
	result := db.Limit(validate.PerPage).Offset(offset).Order("created_at desc").Find(&openapiAuths)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, nil, 0
	}
	return true, openapiAuths, count
}

func (*OpenApiService) AddOpenapiAuth(openapiAuth models.TpOpenapiAuth) (models.TpOpenapiAuth, error) {
	result := psql.Mydb.Create(&openapiAuth)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return openapiAuth, result.Error
	}
	return openapiAuth, nil
}

func (s *OpenApiService) EditOpenApiAuth(validate valid.OpenapiAuthValidate) error {
	openapiAuth := models.TpOpenapiAuth{}
	query := psql.Mydb.Model(openapiAuth)
	query.Where("id=?", validate.Id).First(&openapiAuth)
	result := query.Updates(&validate)
	if result.Error != nil {
		logs.Error(result.Error.Error(), gorm.ErrRecordNotFound)
	}
	return result.Error
}

func (s *OpenApiService) DelOpenApiAuthById(id string) error {
	query := psql.Mydb.Where("id = ?", id)
	result := query.Delete(models.TpOpenapiAuth{})
	if result.Error != nil {
		logs.Error(result.Error.Error(), gorm.ErrRecordNotFound)
	}
	return result.Error
}

func (s *OpenApiService) GetApiList(validate valid.ApiSearchValidate) (bool, []models.TpApi) {
	apis := []models.TpApi{}
	db := psql.Mydb.Model(&models.TpApi{})
	if validate.ApiType != "" {
		db.Where("api_type = ?", validate.ApiType)
	}
	if validate.ServiceType != "" {
		db.Where("service_type = ?", validate.ServiceType)
	}
	if validate.Name != "" {
		db.Where("name like ?", "%"+validate.Name+"%")
	}
	result := db.Find(&apis)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, nil
	}
	rapis := s.GetROpenApiByAuthId(validate.TpOpenapiAuthId)
	// 判断是否已授权 isAdd 1 已添加 0 未添加
	for i, api := range apis {
		for _, ra := range rapis {
			if api.ID == ra.TpApiId {
				apis[i].IsAdd = 1
			}
		}
	}
	return true, apis
}

func (s *OpenApiService) AddApi(api models.TpApi) (models.TpApi, error) {
	result := psql.Mydb.Create(&api)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
	}
	return api, result.Error
}

func (s *OpenApiService) EditApi(validate valid.ApiValidate) error {
	api := models.TpApi{}
	query := psql.Mydb.Model(api)
	query.Where("id=?", validate.Id).First(&api)
	result := query.Updates(&validate)
	if result.Error != nil {
		logs.Error(result.Error.Error(), gorm.ErrRecordNotFound)
	}
	return result.Error
}

func (s *OpenApiService) DelApiById(id string) error {
	query := psql.Mydb.Where("id = ?", id)
	result := query.Delete(models.TpApi{})
	if result.Error != nil {
		logs.Error(result.Error.Error(), gorm.ErrRecordNotFound)
	}
	return result.Error
}

// 生成 Signature
func (*OpenApiService) GenerateAppSecretSignatureHash(secretKey string, signatureMode string, timestamp string) string {
	signatureSource := timestamp + secretKey

	var signature string
	if signatureMode == "SHA256" {
		// 用SHA256进行哈希
		hash := sha256.New()
		hash.Write([]byte(signatureSource))
		hashed := hash.Sum(nil)

		// 将哈希结果转换为十六进制字符串
		signature = hex.EncodeToString(hashed)
	} else if signatureMode == "MD5" {
		md5New := md5.New()
		md5New.Write([]byte(signatureSource))
		// hex转字符串
		signature = hex.EncodeToString(md5New.Sum(nil))
	}

	return signature
}

func (*OpenApiService) GenerateKey() string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (s *OpenApiService) AddROpenApi(validate valid.AddROpenApiValidate) error {

	err := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		for _, r := range validate.TpApiId {
			rapi := models.TpROpenapiAuthApi{
				ID:              utils.GetUUID(),
				TpOpenapiAuthId: validate.TpOpenapiAuthId,
				TpApiId:         r,
			}
			if err := tx.Create(rapi).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		logs.Error(err)
	}
	return err
}

func (s *OpenApiService) EditROpenApi(validate valid.AddROpenApiValidate) error {

	err := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		//先删除
		query := tx.Where("tp_openapi_auth_id = ?", validate.TpOpenapiAuthId)
		if err := query.Delete(models.TpROpenapiAuthApi{}).Error; err != nil {
			return err
		}
		//再添加
		for _, r := range validate.TpApiId {
			rapi := models.TpROpenapiAuthApi{
				ID:              utils.GetUUID(),
				TpOpenapiAuthId: validate.TpOpenapiAuthId,
				TpApiId:         r,
			}

			if err := tx.Create(rapi).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		logs.Error(err)
	}
	return err
}

func (s *OpenApiService) DelROpenApi(validate valid.ROpenApiValidate) error {
	err := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		for _, r := range validate.TpApiId {
			query := tx.Where("tp_openapi_auth_id = ? and tp_api_id = ?", validate.TpOpenapiAuthId, r)
			if err := query.Delete(models.TpROpenapiAuthApi{}).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		logs.Error(err)
	}
	return err
}

func (s *OpenApiService) GetOpenApiAuth(key string) models.TpOpenapiAuth {
	db := psql.Mydb.Model(&models.TpOpenapiAuth{})
	apiAuth := models.TpOpenapiAuth{}
	db.Where("app_key = ?", key).First(&apiAuth)
	return apiAuth
}

func (s *OpenApiService) GetApiListByAuthId(id string) []models.TpApi {
	rapis := s.GetROpenApiByAuthId(id)
	apis := []models.TpApi{}
	db := psql.Mydb.Model(models.TpApi{})
	for _, rapi := range rapis {
		api := models.TpApi{}
		db.Where("id = ?", rapi.TpApiId).First(&api)
		apis = append(apis, api)
	}
	return apis
}

func (s *OpenApiService) GetROpenApiByAuthId(id string) []models.TpROpenapiAuthApi {
	rapis := []models.TpROpenapiAuthApi{}
	db := psql.Mydb.Model(models.TpROpenapiAuthApi{})
	db.Where("tp_openapi_auth_id = ?", id).Find(&rapis)
	return rapis
}

func (s *OpenApiService) AddRDevice(data valid.RDeviceAddValidate) error {
	rdevice := models.TpROpenapiAuthDevice{
		ID:              utils.GetUUID(),
		TpOpenapiAuthId: data.TpOpenapiAuthId,
		DeviceId:        data.DeviceId,
	}
	if err := psql.Mydb.Create(rdevice).Error; err != nil {
		logs.Error(err)
		return err
	}
	return nil
}

func (s *OpenApiService) EditRDevice(data valid.RDeviceValidate) error {

	err := psql.Mydb.Transaction(func(tx *gorm.DB) error {
		//先删除
		for _, d := range data.DeviceId {
			query := tx.Where("tp_openapi_auth_id = ? and device_id = ?", data.TpOpenapiAuthId, d)
			if err := query.Delete(models.TpROpenapiAuthDevice{}).Error; err != nil {
				return err
			}
			rdevice := models.TpROpenapiAuthDevice{
				ID:              utils.GetUUID(),
				TpOpenapiAuthId: data.TpOpenapiAuthId,
				DeviceId:        d,
			}
			if err := tx.Create(rdevice).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		logs.Error(err)
	}
	return err
}

func (s *OpenApiService) DeleteRDevice(data valid.RDeviceAddValidate) error {
	query := psql.Mydb.Where("device_id = ?  and tp_openapi_auth_id = ?", data.DeviceId, data.TpOpenapiAuthId)
	result := query.Delete(models.TpROpenapiAuthDevice{})
	if result.Error != nil {
		logs.Error(result.Error.Error(), gorm.ErrRecordNotFound)
	}
	return result.Error
}

func (s *OpenApiService) GetAuthDevicesByAuthId(id string) []models.TpROpenapiAuthDevice {
	rapis := []models.TpROpenapiAuthDevice{}
	db := psql.Mydb.Model(models.TpROpenapiAuthDevice{})
	db.Where("tp_openapi_auth_id = ?", id).Find(&rapis)
	return rapis
}
