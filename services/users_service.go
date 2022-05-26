package services

import (
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"time"

	"ThingsPanel-Go/initialize/psql"
	bcrypt "ThingsPanel-Go/utils"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type UserService struct {
}

type PaginateUser struct {
	ID              string `json:"id"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	Enabled         string `json:"enabled"`
	AdditionalInfo  string `json:"additional_info"`
	Authority       string `json:"authority"`
	CustomerID      string `json:"customer_id"`
	Email           string `json:"email"`
	Name            string `json:"name"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	SearchText      string `json:"search_text"`
	EmailVerifiedAt int64  `json:"email_verified_at"`
	Mobile          string `json:"mobile"`
	Remark          string `json:"remark"`
	IsAdmin         int64  `json:"is_admin"`
	BusinessId      string `json:"business_id"`
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
func (*UserService) GetUserByEmail(email string) (*models.Users, int64) {
	var users models.Users
	result := psql.Mydb.Where("email = ?", email).First(&users)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &users, result.RowsAffected
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

// Paginate 分页获取user数据
func (*UserService) Paginate(name string, offset int, pageSize int) ([]PaginateUser, int64) {
	var users []PaginateUser
	var count int64
	if name != "" {
		result := psql.Mydb.Model(&models.Users{}).Where("name LIKE ?", "%"+name+"%").Limit(pageSize).Offset(offset).Find(&users)
		psql.Mydb.Model(&models.Users{}).Where("name LIKE ?", "%"+name+"%").Count(&count)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
		}
		if len(users) == 0 {
			users = []PaginateUser{}
		}
		return users, count
	} else {
		result := psql.Mydb.Model(&models.Users{}).Limit(pageSize).Offset(offset).Find(&users)
		psql.Mydb.Model(&models.Users{}).Count(&count)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
		}
		if len(users) == 0 {
			users = []PaginateUser{}
		}
		return users, count
	}
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

// 根据ID编辑一条user数据
func (*UserService) Edit(id string, name string, email string, mobile string, remark string) bool {
	result := psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":       name,
		"email":      email,
		"mobile":     mobile,
		"remark":     remark,
		"updated_at": time.Now().Unix(),
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 根据ID删除一条user数据
func (*UserService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.Users{})
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
