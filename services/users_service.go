package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	bcrypt "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"errors"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type UserService struct {
}

type PaginateUser struct {
	ID              string   `json:"id"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
	Enabled         string   `json:"enabled"`
	AdditionalInfo  string   `json:"additional_info"`
	Authority       string   `json:"authority"`
	CustomerID      string   `json:"customer_id"`
	Email           string   `json:"email"`
	Name            string   `json:"name"`
	FirstName       string   `json:"first_name"`
	LastName        string   `json:"last_name"`
	SearchText      string   `json:"search_text"`
	EmailVerifiedAt int64    `json:"email_verified_at"`
	Mobile          string   `json:"mobile"`
	Remark          string   `json:"remark"`
	IsAdmin         int64    `json:"is_admin"`
	BusinessId      string   `json:"business_id"`
	Roles           []string `json:"roles"`
}

// GetUserByName 根据name获取一条user数据
func (*UserService) GetUserByName(name string) (*models.Users, int64) {
	var users models.Users
	result := psql.Mydb.Where("name = ?", name).First(&users)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &users, result.RowsAffected
}

// GetSameUserByName 根据name获取一条同名称的user数据
func (*UserService) GetSameUserByName(name string, id string) (*models.Users, int64) {
	var users models.Users
	result := psql.Mydb.Where("name = ? AND id <> ?", name, id).First(&users)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &users, result.RowsAffected
}

// GetUserByEmail 根据email获取一条user数据
func (*UserService) GetUserByEmail(email string) (*models.Users, int64, error) {
	var users models.Users
	result := psql.Mydb.Where("email = ?", email).First(&users)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, 0, errors.New("该用户不存在！")
		}
		return nil, 0, result.Error

	}
	return &users, result.RowsAffected, nil
}

// 登录判断，根据email获取一条未删除的user数据
func (*UserService) GetEnabledUserByEmail(email string) (*models.Users, int64, error) {
	var users models.Users
	result := psql.Mydb.Where("email = ? and enabled = '1'", email).First(&users)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, 0, errors.New("该用户不存在！")
		}
		return nil, 0, result.Error
	}
	return &users, result.RowsAffected, nil
}

// GetSameUserByEmail 根据email获取一条同名称的user数据
func (*UserService) GetSameUserByEmail(email string, id string) (*models.Users, int64) {
	var users models.Users
	result := psql.Mydb.Where("email = ? AND id <> ?", email, id).First(&users)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &users, result.RowsAffected
}

// GetUserById 根据id获取一条user数据
func (*UserService) GetUserById(id string) (*models.Users, int64) {
	var users models.Users
	result := psql.Mydb.Where("id = ?", id).First(&users)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &users, result.RowsAffected
}

// 通过id获取用户权限和租户id
func (*UserService) GetUserAuthorityById(id string) (string, string, error) {
	var users models.Users
	result := psql.Mydb.Where("id = ?", id).First(&users)
	if result.Error != nil {
		return "", "", result.Error
	}
	return users.Authority, users.TenantID, nil
}

// 通过用户id和租户id获取用户信息
func (*UserService) GetUserByIdAndTenantId(id string, tenantId string) (*models.Users, error) {
	var users models.Users
	result := psql.Mydb.Where("id = ? AND tenant_id = ?", id, tenantId).First(&users)
	if result.Error != nil {
		return nil, result.Error
	}
	return &users, nil
}

// Paginate 分页获取user数据
func (*UserService) Paginate(name string, offset int, pageSize int, authority string, tenantId string) ([]PaginateUser, int64, error) {
	var users []PaginateUser
	var count int64
	tx := psql.Mydb.Model(&models.Users{})
	if name != "" {
		tx.Where("name LIKE ?", "%"+name+"%")
	}
	if tenantId != "" {
		tx.Where("name = ?", tenantId)
	}
	if authority != "" {
		tx.Where("authority = ?", authority)
	} else {
		return users, 0, errors.New("权限不足！")
	}
	if err := tx.Count(&count).Error; err != nil {
		logs.Info(err)
		return users, 0, err
	}
	if err := tx.Limit(pageSize).Offset(offset).Find(&users).Error; err != nil {
		logs.Info(err)
		return users, 0, err
	}
	if len(users) != 0 {
		var CasbinService CasbinService
		for index, user := range users {
			roles, _ := CasbinService.GetRoleFromUser(user.Email)
			users[index].Roles = roles
		}
	}
	return users, count, nil
}

// Add新增一条user数据
func (*UserService) Add(name string, email string, password string, enabled string, mobile string, remark string) (bool, string) {
	var uuid = uuid.GetUuid()
	pass := bcrypt.HashAndSalt([]byte(password))
	user := models.Users{
		ID:        uuid,
		Name:      name,
		Email:     email,
		Password:  pass,
		Enabled:   enabled,
		Mobile:    mobile,
		Remark:    remark,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	result := psql.Mydb.Create(&user)
	if result.Error != nil {
		logs.Info(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

// 添加用户
func (*UserService) AddUser(user valid.AddUser) (userModel models.Users, err error) {
	var uuid = uuid.GetUuid()
	pass := bcrypt.HashAndSalt([]byte(user.Password))
	userModel = models.Users{
		ID:             uuid,
		Name:           user.Name,
		Email:          user.Email,
		Password:       pass,
		Enabled:        "1",
		Mobile:         user.Mobile,
		Remark:         user.Remark,
		CreatedAt:      time.Now().Unix(),
		UpdatedAt:      time.Now().Unix(),
		Authority:      user.Authority,
		AdditionalInfo: user.AdditionalInfo,
		TenantID:       user.TenantID,
	}
	result := psql.Mydb.Create(&userModel)
	if result.Error != nil {
		return userModel, result.Error
	}
	return userModel, result.Error
}

// 根据ID编辑一条user数据
func (*UserService) Edit(id string, name string, email string, mobile string, remark string, enabled string) bool {
	result := psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":       name,
		"email":      email,
		"mobile":     mobile,
		"remark":     remark,
		"updated_at": time.Now().Unix(),
		"enabled":    enabled,
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据ID删除一条user数据
func (*UserService) Delete(id string) bool {
	result := psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Update("enabled", "A")
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 修改密码
func (*UserService) Password(id string, password string) bool {
	pass := bcrypt.HashAndSalt([]byte(password))
	result := psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Update("password", pass)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// Register注册一条user数据
func (*UserService) Register(email string, name string, password string, customer_id string) (bool, string) {
	var uuid = uuid.GetUuid()
	pass := bcrypt.HashAndSalt([]byte(password))
	user := models.Users{
		ID:         uuid,
		Email:      email,
		Name:       name,
		Password:   pass,
		CustomerID: customer_id,
		CreatedAt:  time.Now().Unix(),
		UpdatedAt:  time.Now().Unix(),
	}
	result := psql.Mydb.Create(&user)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	return true, uuid
}

// 判断是否有添加用户的权限：SYS_ADMIN只能添加TENANT_ADMIN，TENANT_ADMIN和TENANT_USER只能添加TENANT_USER
func (*UserService) HasAddAuthority(authority string, add_authority string) bool {
	if authority == "SYS_ADMIN" && add_authority == "TENANT_ADMIN" {
		return true
	}
	if (authority == "TENANT_ADMIN" || authority == "TENANT_USER") && add_authority == "TENANT_USER" {
		return true
	}
	return false
}

// 判断是否有编辑用户的权限：SYS_ADMIN只能编辑TENANT_ADMIN，TENANT_ADMIN和TENANT_USER只能编辑TENANT_USER
func (*UserService) HasEditAuthority(authority string, edit_authority string) bool {
	if authority == "SYS_ADMIN" && edit_authority == "TENANT_ADMIN" {
		return true
	}
	if (authority == "TENANT_ADMIN" || authority == "TENANT_USER") && edit_authority == "TENANT_USER" {
		return true
	}
	return false
}
