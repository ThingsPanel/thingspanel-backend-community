package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
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
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return &users, result.RowsAffected
}

// GetSameUserByName 根据name获取一条同名称的user数据
func (*UserService) GetSameUserByName(name string, id string) (*models.Users, int64) {
	var users models.Users
	result := psql.Mydb.Where("name = ? AND id <> ?", name, id).First(&users)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, 0
		}
		logs.Error(result.Error.Error())
		return nil, 0
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

// 租户注册
func (*UserService) TenantRegister(reqData valid.TenantRegisterValidate) (*models.Users, error) {
	var users models.Users
	// 从redis中获取验证码
	redisCode := redis.GetStr(reqData.PhoneNumber + "_code")
	if redisCode != reqData.VerificationCode {
		if reqData.VerificationCode != "ThingsPanel" {
			return &users, errors.New("验证码错误！")
		}
	}
	// 重复标志
	repeatFlag := true
	// 判断手机号是否重复
	result := psql.Mydb.Where("mobile = ?", reqData.PhoneNumber).First(&users)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			repeatFlag = false
		} else {
			return &users, result.Error
		}
	}
	if repeatFlag {
		return &users, errors.New("该手机号已注册！")
	}
	repeatFlag = true
	// 判断手机号是否重复
	result = psql.Mydb.Where("email = ?", reqData.Email).First(&users)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			repeatFlag = false
		} else {
			return &users, result.Error
		}
	}
	if repeatFlag {
		return &users, errors.New("该邮箱已注册！")
	}
	// 创建租户
	var uuid = uuid.GetUuid()
	pass := bcrypt.HashAndSalt([]byte(reqData.Password))
	user := models.Users{
		ID:        uuid,
		Name:      reqData.Email,
		Email:     reqData.Email,
		Password:  pass,
		Enabled:   "1",
		Mobile:    reqData.PhoneNumber,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Authority: "TENANT_ADMIN",
		TenantID:  uuid[:7],
	}
	result = psql.Mydb.Create(&user)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return &user, result.Error
	}
	return &user, nil
}

// 登录
func (*UserService) Login(reqData valid.LoginValidate) (*models.Users, error) {
	var result *gorm.DB
	var users models.Users
	// 判断是邮箱登录还是手机号登录
	if reqData.Email != "" {
		// 判断密码是否为空
		if reqData.Password == "" {
			return &users, errors.New("密码不能为空！")
		}
		// 校验email的格式
		if err := utils.CheckEmail(reqData.Email); err != nil {
			return &users, errors.New("邮箱格式不正确！")
		}
		result = psql.Mydb.Where("email = ?", reqData.Email).First(&users)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return &users, errors.New("该用户不存在！")
			}
			return &users, result.Error
		}
		// 校验密码
		if !bcrypt.ComparePasswords(users.Password, []byte(reqData.Password)) {
			return &users, errors.New("密码错误！")
		}
	} else if reqData.PhoneNumber != "" {
		// 判断验证码还是密码登录
		if reqData.VerificationCode != "" {
			// 从redis中获取验证码
			redisCode := redis.GetStr(reqData.PhoneNumber + "_code")
			if redisCode != reqData.VerificationCode {
				return &users, errors.New("验证码错误！")
			}
			result = psql.Mydb.Where("mobile = ?", reqData.PhoneNumber).First(&users)
			if result.Error != nil {
				if errors.Is(result.Error, gorm.ErrRecordNotFound) {
					return &users, errors.New("该用户不存在！")
				}
				return &users, result.Error
			}
		} else {
			// 判断密码是否为空
			if reqData.Password == "" {
				return &users, errors.New("密码不能为空！")
			}
			result = psql.Mydb.Where("mobile = ?", reqData.PhoneNumber).First(&users)
			if result.Error != nil {
				if errors.Is(result.Error, gorm.ErrRecordNotFound) {
					return &users, errors.New("该用户不存在！")
				}
				return &users, result.Error
			}
			// 校验密码
			if !bcrypt.ComparePasswords(users.Password, []byte(reqData.Password)) {
				logs.Error("密码错误！", users.Password, reqData.Password)
				return &users, errors.New("密码错误！")
			}
		}
	} else {
		return &users, errors.New("账号不能为空！")
	}
	// 判断用户状态
	if users.Enabled == "0" {
		return &users, errors.New("账户状态异常，请联系管理员！")
	}
	return &users, nil
}

// 修改密码
func (*UserService) ChangePassword(reqData valid.ChangePasswordValidate) (*models.Users, error) {
	var users models.Users
	// 从redis中获取验证码
	redisCode := redis.GetStr(reqData.PhoneNumber + "_code")
	if redisCode != reqData.VerificationCode || redisCode == "" {
		return &users, errors.New("验证码错误！")
	}

	// 监测上次请求时间，防爆破
	lastRequestTime := redis.GetStr(reqData.PhoneNumber + "_change_password")
	if lastRequestTime != "" {
		return &users, errors.New("验证码校验错误！")

	}

	// 设置上次请求时间标志位
	err := redis.SetStr(reqData.PhoneNumber+"_change_password", "1", 1*time.Minute)
	if err != nil {
		return &users, errors.New("验证码校验设置错误！")
	}

	// 查询用户是否存在
	result := psql.Mydb.Where("mobile = ?", reqData.PhoneNumber).First(&users)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return &users, errors.New("该手机号未注册！")
		} else {
			return &users, result.Error
		}
	}
	users.Password = bcrypt.HashAndSalt([]byte(reqData.Password))
	result = psql.Mydb.Save(&users)

	if result.Error != nil {
		logs.Error(result.Error.Error())
		return &users, result.Error
	}
	return &users, nil
}

// 登录判断，根据email获取一条未删除的user数据
func (*UserService) GetEnabledUserByEmail(email string) (*models.Users, int64, error) {
	var users models.Users
	var result *gorm.DB
	// 校验email的格式
	if err := utils.CheckEmail(email); err == nil {
		result = psql.Mydb.Where("email = ? and enabled = '1'", email).First(&users)
	} else {
		result = psql.Mydb.Where("mobile = ? and enabled = '1'", email).First(&users)
	}

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
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return &users, result.RowsAffected
}

// GetUserById 根据id获取一条user数据
func (*UserService) GetUserById(id string) (*models.Users, int64) {
	var users models.Users
	result := psql.Mydb.Where("id = ?", id).First(&users)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return nil, 0
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

// Paginate 分页获取user数据
func (*UserService) Paginate(name string, offset int, pageSize int, authority string, tenantId string) ([]PaginateUser, int64, error) {
	var paginateUsers []PaginateUser
	var count int64
	tx := psql.Mydb.Model(&models.Users{})
	tx.Where("enabled = '1'")
	if name != "" {
		tx.Where("name LIKE ?", "%"+name+"%")
	}
	if tenantId != "" {
		tx.Where("tenant_id = ?", tenantId)
	}
	if authority != "" {
		tx.Where("authority = ?", authority)
	} else {
		return paginateUsers, 0, errors.New("权限不足！")
	}
	if err := tx.Count(&count).Error; err != nil {
		logs.Info(err)
		return paginateUsers, 0, err
	}
	if err := tx.Limit(pageSize).Offset(offset).Scan(&paginateUsers).Error; err != nil {
		logs.Error(err)
		return paginateUsers, 0, err
	}
	if len(paginateUsers) != 0 {
		var CasbinService CasbinService
		for index, user := range paginateUsers {
			roles, _ := CasbinService.GetRoleFromUser(user.Email)
			paginateUsers[index].Roles = roles
		}
	}
	return paginateUsers, count, nil
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
		logs.Error(result.Error.Error())
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

	var result *gorm.DB
	if len(enabled) == 0 {
		result = psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Updates(map[string]interface{}{
			"name":       name,
			"email":      email,
			"mobile":     mobile,
			"remark":     remark,
			"updated_at": time.Now().Unix(),
		})
	} else {
		result = psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Updates(map[string]interface{}{
			"name":       name,
			"email":      email,
			"mobile":     mobile,
			"remark":     remark,
			"updated_at": time.Now().Unix(),
			"enabled":    enabled,
		})
	}
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

// 根据ID删除一条user数据
func (*UserService) Delete(id string) bool {
	result := psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Update("enabled", "0")
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

// 修改密码
func (*UserService) Password(id string, password string) bool {
	pass := bcrypt.HashAndSalt([]byte(password))
	result := psql.Mydb.Model(&models.Users{}).Where("id = ?", id).Update("password", pass)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
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
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
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

func (*UserService) CountUsers(authority, tenantId string) (int64, error) {
	var count int64
	if authority == "SYS_ADMIN" {
		result := psql.Mydb.Model(&models.Users{}).Where("authority = ? and enabled = '1'", "TENANT_ADMIN").Count(&count)
		if result.Error != nil {
			return count, result.Error
		}
	} else if authority == "TENANT_ADMIN" {
		result := psql.Mydb.Model(&models.Users{}).Where("authority = ? and enabled = '1' and tenant_id = ?", "TENANT_USER", tenantId).Count(&count)
		if result.Error != nil {
			return count, result.Error
		}
	}
	return count, nil
}

func (*UserService) GetTenantConfigByTenantId(tenantId string) (models.TpTenantConfig, error) {
	var config models.TpTenantConfig
	result := psql.Mydb.Model(&models.TpTenantConfig{}).Where("tenant_id = ? ", tenantId).Find(&config)
	return config, result.Error
}
