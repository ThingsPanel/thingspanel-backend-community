package service

import (
	"context"
	"errors"
	tpErrors "project/internal/errors"

	"fmt"
	"project/pkg/common"
	"strings"
	"time"

	"gorm.io/gorm"

	"project/initialize"
	dal "project/internal/dal"
	"project/internal/logic"
	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type User struct{}

// @description  创建用户
func (u *User) CreateUser(createUserReq *model.CreateUserReq, claims *utils.UserClaims) error {

	var user = model.User{}
	// uuid生成用户id
	user.ID = uuid.New()
	user.Name = createUserReq.Name
	user.PhoneNumber = createUserReq.PhoneNumber
	user.Email = createUserReq.Email
	user.Status = StringPtr("N")
	user.Remark = createUserReq.Remark

	// 其他信息
	if createUserReq.AdditionalInfo == nil {
		user.AdditionalInfo = StringPtr("{}")
	} else {
		user.AdditionalInfo = createUserReq.AdditionalInfo
	}
	// 判断用户权限
	if claims.Authority == "SYS_ADMIN" { // 系统管理员创建租户管理员
		user.Authority = StringPtr("TENANT_ADMIN")
		user.TenantID = StringPtr(strings.Split(uuid.New(), "-")[0])
	} else if claims.Authority == "TENANT_ADMIN" { // 租户管理员创建租户用户
		user.Authority = StringPtr("TENANT_USER")
		a, err := u.GetUserById(claims.ID)
		if err != nil {
			logrus.Error(err)
			return err
		}
		user.TenantID = a.TenantID
	}
	t := time.Now().UTC()
	user.CreatedAt = &t
	user.UpdatedAt = &t
	user.PasswordLastUpdated = &t

	// 生成密码
	user.Password = utils.BcryptHash(createUserReq.Password)
	err := dal.CreateUsers(&user)
	if err != nil {
		logrus.Error(err)
		if strings.Contains(err.Error(), "users_un") {
			return fmt.Errorf("email already exists")
		}
		return err
	}
	if createUserReq.RoleIDs != nil && len(createUserReq.RoleIDs) > 0 {
		// 绑定角色
		ok := GroupApp.Casbin.AddRolesToUser(user.ID, createUserReq.RoleIDs)
		if !ok {
			logrus.Error(err)
			return fmt.Errorf("add role to user failed")
		}
	}
	return err
}

// @description  用户登录
func (u *User) Login(ctx context.Context, loginReq *model.LoginReq) (*model.LoginRsp, error) {
	// 通过邮箱获取用户信息
	user, err := dal.GetUsersByEmail(loginReq.Email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, tpErrors.NewError(tpErrors.ErrInvalidCredentials)
		}
		return nil, tpErrors.Wrap(err, tpErrors.ErrDatabaseError)
	}
	// 是否加密配置
	if logic.UserIsEncrypt(ctx) {
		password, err := initialize.DecryptPassword(loginReq.Password)
		if err != nil {
			return nil, fmt.Errorf("wrong decrypt password")
		}
		passwords := strings.TrimSuffix(string(password), loginReq.Salt)
		loginReq.Password = passwords
	}
	// 对比密码
	if !utils.BcryptCheck(loginReq.Password, user.Password) {
		return nil, tpErrors.NewError(tpErrors.ErrInvalidCredentials)
	}

	// 判断用户状态
	if *user.Status != "N" {
		return nil, tpErrors.NewError(tpErrors.ErrUserDisabled)

	}

	logrsp, err := u.UserLoginAfter(user)
	if err != nil {
		return nil, err
	}

	// 更新登录时间
	err = dal.UserQuery{}.UpdateLastVisitTime(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return logrsp, nil
}

// UserLoginAfter
// @description 用户登录后token获取保存
func (*User) UserLoginAfter(user *model.User) (*model.LoginRsp, error) {
	key := viper.GetString("jwt.key")
	// 生成token
	jwt := utils.NewJWT([]byte(key))
	claims := utils.UserClaims{
		ID:         user.ID,
		Email:      user.Email,
		Authority:  *user.Authority,
		CreateTime: time.Now().UTC(),
		TenantID:   *user.TenantID,
	}
	token, err := jwt.GenerateToken(claims)
	if err != nil {
		return nil, tpErrors.Wrap(err, tpErrors.ErrSystemInternal)

	}
	timeout := viper.GetInt("session.timeout")
	reset_on_request := viper.GetBool("session.reset_on_request")
	if reset_on_request {
		if timeout == 0 {
			// 过期时间为1小时
			timeout = 60
		}
	}
	// 保存token到redis
	global.REDIS.Set(token, "1", time.Duration(timeout)*time.Minute)
	// 禁止共享token，这里永久存储账号和token的关系，是可以保证一个账号只能在一个地方登录
	if !logic.UserIsShare(context.Background()) {
		oldToken, err := global.REDIS.Get(user.Email + "_token").Result()
		if err != nil {
			logrus.Error(err)
		} else {
			global.REDIS.Del(oldToken)
		}
		global.REDIS.Set(user.Email+"_token", token, 0)
	}

	loginRsp := &model.LoginRsp{
		Token:     &token,
		ExpiresIn: int64(time.Duration(timeout) * time.Minute),
	}
	return loginRsp, nil
}

// @description 退出登录
func (*User) Logout(token string) error {
	// 删除redis中的token
	global.REDIS.Del(token)
	return nil
}

// @description 刷新token
func (*User) RefreshToken(userClaims *utils.UserClaims) (*model.LoginRsp, error) {
	// 通过邮箱获取用户信息
	user, err := dal.GetUsersByEmail(userClaims.Email)
	if err != nil {
		return nil, err
	}

	// 判断用户状态
	if *user.Status != "N" {
		return nil, fmt.Errorf("user status exception")
	}

	key := viper.GetString("jwt.key")
	// 生成token
	jwt := utils.NewJWT([]byte(key))
	claims := utils.UserClaims{
		ID:         user.ID,
		Email:      user.Email,
		Authority:  *user.Authority,
		CreateTime: time.Now().UTC(),
		TenantID:   *user.TenantID,
	}
	token, err := jwt.GenerateToken(claims)
	if err != nil {
		return nil, err
	}

	global.REDIS.Set(token, "1", 24*7*time.Hour)

	loginRsp := &model.LoginRsp{
		Token:     &token,
		ExpiresIn: int64(24 * 7 * time.Hour.Seconds()),
	}
	return loginRsp, nil
}

// @description 发送验证码
func (*User) GetVerificationCode(email, isRegister string) error {
	// 通过邮箱获取用户信息
	user, err := dal.GetUsersByEmail(email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logrus.Error(err)
		return err
	}
	switch {
	case user == nil && isRegister != "1":
		return fmt.Errorf("email does not exist")
	case user != nil && isRegister == "1":
		return fmt.Errorf("email already exists")
	}
	verificationCode, err := common.GenerateNumericCode(6)
	if err != nil {
		return err
	}
	err = global.REDIS.Set(email+"_code", verificationCode, 5*time.Minute).Err()
	if err != nil {
		return err
	}
	logrus.Warningf("验证码:%s", verificationCode)
	err = GroupApp.NotificationServicesConfig.SendTestEmail(&model.SendTestEmailReq{
		Email: email,
		Body:  fmt.Sprintf("Your verification code is %s", verificationCode),
	})
	return err
}

// @description ResetPassword By VerifyCode and Email
func (*User) ResetPassword(ctx context.Context, resetPasswordReq *model.ResetPasswordReq) error {

	err := utils.ValidatePassword(resetPasswordReq.Password)
	if err != nil {
		return err
	}

	verificationCode, err := global.REDIS.Get(resetPasswordReq.Email + "_code").Result()
	if err != nil {
		return fmt.Errorf("verification code expired")
	}
	if verificationCode != resetPasswordReq.VerifyCode {
		return fmt.Errorf("verification code error")
	}

	var (
		db   = dal.UserQuery{}
		user = query.User
	)
	info, err := db.First(ctx, user.Email.Eq(resetPasswordReq.Email))
	if err != nil {
		logrus.Error(ctx, "[ResetPasswordByCode]Get Users info failed:", err)
		return err
	}
	t := time.Now().UTC()
	info.PasswordLastUpdated = &t
	info.Password = utils.BcryptHash(resetPasswordReq.Password)
	if err = db.UpdateByEmail(ctx, info, user.Password, user.PasswordLastUpdated); err != nil {
		logrus.Error(ctx, "[ResetPasswordByCode]Update Users info failed:", err)
		return err
	}
	return nil
}

// @description  通过id获取用户信息
func (*User) GetUserById(id string) (*model.User, error) {
	user, err := dal.GetUsersById(id)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// @description  分页获取用户列表
func (*User) GetUserListByPage(userListReq *model.UserListReq, claims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetUserListByPage(userListReq, claims)
	if err != nil {
		return nil, err
	}
	userListRspMap := make(map[string]interface{})
	userListRspMap["total"] = total
	userListRspMap["list"] = list
	return userListRspMap, nil
}

// @description  修改用户信息
func (*User) UpdateUser(updateUserReq *model.UpdateUserReq, claims *utils.UserClaims) error {
	//密码不能小于6位，如果等于空则不修改密码
	if updateUserReq.Password != nil {
		if len(*updateUserReq.Password) == 0 {
			updateUserReq.Password = nil
		} else if len(*updateUserReq.Password) < 6 {
			return fmt.Errorf("password length must be greater than 6")
		}
	}

	user, err := dal.GetUsersById(updateUserReq.ID)
	if err != nil {
		return err
	}

	// 判断用户权限，租户管理员和租户用户不能修改其他租户的信息
	if claims.Authority == "TENANT_ADMIN" || claims.Authority == "TENANT_USER" {
		if *user.TenantID != claims.TenantID {
			return fmt.Errorf("you cannot modify information about other tenants")
		}

		// 租户管理员不能修改自己的状态
		if claims.Authority == "TENANT_ADMIN" && *user.Authority == "TENANT_ADMIN" && *user.Status != *updateUserReq.Status {
			if updateUserReq.Status != nil {
				if updateUserReq.Status != nil {
					return fmt.Errorf("tenant administrators cannot change their own status")
				}
			}
		}
	}

	t := time.Now().UTC()
	// 密码修改特殊处理
	if updateUserReq.Password != nil {
		user.Password = *StringPtr(utils.BcryptHash(*updateUserReq.Password))
		user.PasswordLastUpdated = &t
	}

	user.UpdatedAt = &t
	user.Name = updateUserReq.Name
	user.PhoneNumber = *updateUserReq.PhoneNumber
	user.AdditionalInfo = updateUserReq.AdditionalInfo
	user.Status = updateUserReq.Status
	user.Remark = updateUserReq.Remark

	_, err = dal.UpdateUserInfoById(claims.ID, user)
	if err != nil {
		return err
	}

	// 修改角色
	if updateUserReq.RoleIDs != nil {
		// 先删除原有角色
		GroupApp.Casbin.RemoveUserAndRole(updateUserReq.ID)
		// 绑定新角色
		if len(updateUserReq.RoleIDs) > 0 {
			ok := GroupApp.Casbin.AddRolesToUser(updateUserReq.ID, updateUserReq.RoleIDs)
			if !ok {
				return fmt.Errorf("update user failed")
			}
		}
	}

	return nil
}

// @description  删除用户
func (*User) DeleteUser(id string, claims *utils.UserClaims) error {
	user, err := dal.GetUsersById(id)
	if err != nil {
		return err
	}

	// 判断用户权限，租户管理员和租户用户不能修改其他租户的信息
	if claims.Authority == "TENANT_ADMIN" || claims.Authority == "TENANT_USER" {
		if *user.TenantID != claims.TenantID {
			return fmt.Errorf("authority exception")
		}
		// 租户管理员不能删除自己
		if claims.Authority == "TENANT_ADMIN" {
			if *user.Authority == "TENANT_ADMIN" {
				return fmt.Errorf("authority exception")
			}
		}
	}

	// 不能删除系统管理员
	if *user.Authority == "SYS_ADMIN" {
		return fmt.Errorf("authority exception")
	}

	err = dal.DeleteUsersById(id)
	if err != nil {
		return err
	}
	// 先删除原有角色
	GroupApp.Casbin.RemoveUserAndRole(id)

	return nil
}

// 获取用户信息
func (*User) GetUser(id string, claims *utils.UserClaims) (*model.User, error) {
	user, err := dal.GetUsersById(id)
	if err != nil {
		return nil, err
	}
	// 判断用户权限，租户管理员和租户用户不能查看其他租户的信息
	if claims.Authority == "TENANT_ADMIN" || claims.Authority == "TENANT_USER" {
		if *user.TenantID != claims.TenantID {
			return nil, fmt.Errorf("authority exception")
		}
	}
	return user, nil
}

// 获取用户详细信息
func (*User) GetUserDetail(claims *utils.UserClaims) (*model.User, error) {
	user, err := dal.GetUsersById(claims.ID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// @description 修改用户信息（只能修改自己）
func (*User) UpdateUserInfo(ctx context.Context, updateUserReq *model.UpdateUserInfoReq, claims *utils.UserClaims) error {
	user, err := dal.GetUsersById(claims.ID)
	if err != nil {
		return err
	}

	// 限制用户只能修改自己的信息
	if user.ID != claims.ID {
		return fmt.Errorf("authority exception")
	}

	// 是否加密配置
	if logic.UserIsEncrypt(ctx) {
		password, err := initialize.DecryptPassword(*updateUserReq.Password)
		if err != nil {
			return fmt.Errorf("wrong decrypt password")
		}
		passwords := strings.TrimSuffix(string(password), updateUserReq.Salt)
		*updateUserReq.Password = passwords
	}

	// 处理修改密码的情况
	if updateUserReq.Password != nil {
		updateUserReq.Password = StringPtr(utils.BcryptHash(*updateUserReq.Password))
	}

	r, err := dal.UpdateUserInfoByIdPersonal(user.ID, updateUserReq)
	if r == 0 {
		return fmt.Errorf("0 rows affected")
	}
	return err
}

// @description SuperAdmin Become Other admin
func (*User) TransformUser(transformUserReq *model.TransformUserReq, claims *utils.UserClaims) (*model.LoginRsp, error) {

	if claims.Authority != "SYS_ADMIN" && claims.Authority != "TENANT_ADMIN" {
		return nil, fmt.Errorf("authority wrong")
	}

	becomeUser, err := dal.GetUsersById(transformUserReq.BecomeUserID)
	if err != nil {
		return nil, err
	}

	// 判断用户状态
	if *becomeUser.Status != "N" {
		return nil, fmt.Errorf("user status unexception  ")
	}

	key := viper.GetString("jwt.key")
	// 生成token
	jwt := utils.NewJWT([]byte(key))
	becomeUserClaims := utils.UserClaims{
		ID:         becomeUser.ID,
		Email:      becomeUser.Email,
		Authority:  *becomeUser.Authority,
		CreateTime: time.Now().UTC(),
		TenantID:   *becomeUser.TenantID,
	}
	token, err := jwt.GenerateToken(becomeUserClaims)
	if err != nil {
		return nil, err
	}

	global.REDIS.Set(token, "1", 24*7*time.Hour)

	loginRsp := &model.LoginRsp{
		Token:     &token,
		ExpiresIn: int64(24 * 7 * time.Hour.Seconds()),
	}
	return loginRsp, nil
}

func (u *User) EmailRegister(ctx context.Context, req *model.EmailRegisterReq) (*model.LoginRsp, error) {
	err := utils.ValidatePassword(req.Password)
	if err != nil {
		return nil, err
	}
	//验证码验证
	verificationCode, err := global.REDIS.Get(req.Email + "_code").Result()
	if err != nil {
		return nil, fmt.Errorf("verification code expired")
	}
	if verificationCode != req.VerifyCode {
		return nil, fmt.Errorf("verification code error")
	}
	if req.Password != req.ConfirmPassword {
		return nil, fmt.Errorf("your confirmed password and new password do not match")
	}
	// 验证邮箱是否注册
	user, err := dal.GetUsersByEmail(req.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("busy network")
	}
	if user != nil {
		return nil, fmt.Errorf("email already exists")
	}
	// 是否加密配置
	if logic.UserIsEncrypt(ctx) {
		if req.Salt == nil {
			return nil, fmt.Errorf("salt is null")
		}
		password, err := initialize.DecryptPassword(req.Password)
		if err != nil {
			return nil, fmt.Errorf("wrong decrypt password")
		}
		passwords := strings.TrimSuffix(string(password), *req.Salt)
		req.Password = passwords
	}
	req.Password = utils.BcryptHash(req.Password)
	now := time.Now().UTC()
	// 有效期+一年
	//periodValidity := now.AddDate(1, 0, 0).UTC()
	// 有效期转字符串2024-07-29T21:20:17.232478+08:00
	//periodValidityStr := periodValidity.Format(time.RFC3339)
	tenantID, err := common.GenerateRandomString(8)
	if err != nil {
		return nil, err
	}
	userInfo := &model.User{
		ID:                  uuid.New(),
		Name:                &req.Email,
		PhoneNumber:         fmt.Sprintf("%s %s", req.PhonePrefix, req.PhoneNumber),
		Email:               req.Email,
		Status:              StringPtr("N"),
		Authority:           StringPtr("TENANT_ADMIN"),
		Password:            req.Password,
		TenantID:            StringPtr(tenantID),
		Remark:              StringPtr(now.Add(365 * 24 * time.Hour).String()),
		CreatedAt:           &now,
		UpdatedAt:           &now,
		PasswordLastUpdated: &now,
		//Remark:      &periodValidityStr,
	}
	err = dal.CreateUsers(userInfo)
	if err != nil {
		return nil, fmt.Errorf("busy network")
	}
	return u.UserLoginAfter(userInfo)
}
