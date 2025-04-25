package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"project/pkg/common"
	"project/pkg/errcode"

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
	user := model.User{}
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
		var js map[string]interface{}
		if err := json.Unmarshal(*createUserReq.AdditionalInfo, &js); err != nil {
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error": fmt.Sprintf("Failed to unmarshal AdditionalInfo: %v", err),
			})
		}
		user.AdditionalInfo = StringPtr(string(*createUserReq.AdditionalInfo))
	}
	// 判断用户权限
	switch claims.Authority {
	case "SYS_ADMIN": // 系统管理员创建租户管理员
		user.Authority = StringPtr("TENANT_ADMIN")
		user.TenantID = StringPtr(strings.Split(uuid.New(), "-")[0])
	case "TENANT_ADMIN": // 租户管理员创建租户用户
		user.Authority = StringPtr("TENANT_USER")
		a, err := u.GetUserById(claims.ID)
		if err != nil {
			logrus.Error(err)
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error":    err.Error(),
				"admin_id": claims.ID,
			})
		}
		user.TenantID = a.TenantID
	default:
		// 权限不足
		return errcode.WithVars(errcode.CodeNoPermission, map[string]interface{}{
			"required_role": "SYS_ADMIN or TENANT_ADMIN",
			"current_role":  claims.Authority,
		})
	}
	t := time.Now().UTC()
	user.CreatedAt = &t
	user.UpdatedAt = &t
	user.PasswordLastUpdated = &t

	// 验证密码格式
	if len(createUserReq.Password) < 6 {
		return errcode.New(200040) // 密码格式错误
	}

	// 生成密码
	hashedPassword := utils.BcryptHash(createUserReq.Password)
	if hashedPassword == "" {
		return errcode.WithData(errcode.CodeDecryptError, map[string]interface{}{
			"error": "Failed to hash password",
		})
	}
	user.Password = hashedPassword

	// 创建用户
	err := dal.CreateUsers(&user)
	if err != nil {
		logrus.Error(err)
		if strings.Contains(err.Error(), "users_un") {
			return errcode.New(200008) // 用户邮箱已注册
		}
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":      err.Error(),
			"user_email": user.Email,
		})
	}

	// 如果是创建租户管理员，则给租户新增一个默认的首页看板
	if claims.Authority == "SYS_ADMIN" {
		err = dal.BoardQuery{}.CreateDefaultBoard(context.Background(), *user.TenantID)
		if err != nil {
			logrus.Error(err)
		}
	}

	// 绑定角色
	if len(createUserReq.RoleIDs) > 0 {
		ok := GroupApp.Casbin.AddRolesToUser(user.ID, createUserReq.RoleIDs)
		if !ok {
			logrus.Error("Failed to add roles to user")
			return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
				"error":    "Failed to add roles to user",
				"user_id":  user.ID,
				"role_ids": createUserReq.RoleIDs,
			})
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
			// 用户不存在,返回用户模块的业务错误
			return nil, errcode.New(errcode.CodeInvalidAuth)
		}
		// 数据库操作失败,返回系统级数据库错误
		return nil, errcode.New(errcode.CodeDBError)
	}
	// 是否加密配置
	if logic.UserIsEncrypt(ctx) {
		password, err := initialize.DecryptPassword(loginReq.Password)
		if err != nil {
			return nil, errcode.New(errcode.CodeDecryptError)
		}
		passwords := strings.TrimSuffix(string(password), loginReq.Salt)
		loginReq.Password = passwords
	}
	// 对比密码
	if !utils.BcryptCheck(loginReq.Password, user.Password) {
		return nil, errcode.New(errcode.CodeInvalidAuth)
	}

	// 判断用户状态
	if *user.Status != "N" {
		return nil, errcode.New(errcode.CodeUserDisabled)
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
		return nil, errcode.New(errcode.CodeTokenGenerateError)
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
	global.REDIS.Set(context.Background(), token, "1", time.Duration(timeout)*time.Minute)
	// 禁止共享token，这里永久存储账号和token的关系，是可以保证一个账号只能在一个地方登录
	if !logic.UserIsShare(context.Background()) {
		oldToken, err := global.REDIS.Get(context.Background(), user.Email+"_token").Result()
		if err != nil {
			logrus.Error(err)
		} else {
			global.REDIS.Del(context.Background(), oldToken)
		}
		global.REDIS.Set(context.Background(), user.Email+"_token", token, 0)
	}

	loginRsp := &model.LoginRsp{
		Token:     &token,
		ExpiresIn: int64(timeout * 60), // 转换为秒数
	}
	return loginRsp, nil
}

// @description 退出登录
func (*User) Logout(token string) error {
	if err := global.REDIS.Del(context.Background(), token).Err(); err != nil {
		return errcode.New(errcode.CodeTokenDeleteError)
	}
	return nil
}

// @description 刷新token
func (*User) RefreshToken(userClaims *utils.UserClaims) (*model.LoginRsp, error) {
	// 通过邮箱获取用户信息
	user, err := dal.GetUsersByEmail(userClaims.Email)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "query_user",
			"email":     userClaims.Email,
			"error":     err.Error(),
		})
	}

	// 判断用户状态
	if *user.Status != "N" {
		return nil, errcode.New(errcode.CodeUserDisabled)
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
		return nil, errcode.New(errcode.CodeTokenGenerateError)
	}

	global.REDIS.Set(context.Background(), token, "1", 24*7*time.Hour)

	loginRsp := &model.LoginRsp{
		Token:     &token,
		ExpiresIn: int64(24 * 7 * time.Hour.Seconds()),
	}
	return loginRsp, nil
}

// @description 发送验证码
func (*User) GetVerificationCode(email, isRegister string) error {
	user, err := dal.GetUsersByEmail(email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "query_user",
			"email":     email,
			"error":     err.Error(),
		})
	}

	// 邮箱验证相关错误应归类到用户模块
	switch {
	case user == nil && isRegister != "1":
		return errcode.New(200007) // 新增: 用户邮箱不存在
	case user != nil && isRegister == "1":
		return errcode.New(200008) // 新增: 用户邮箱已注册
	}

	verificationCode, err := common.GenerateNumericCode(6)
	if err != nil {
		return errcode.WithData(200009, map[string]interface{}{ // 新增: 验证码生成失败
			"email": email,
		})
	}

	err = global.REDIS.Set(context.Background(), email+"_code", verificationCode, 5*time.Minute).Err()
	if err != nil {
		return errcode.WithData(errcode.CodeCacheError, map[string]interface{}{
			"operation": "save_verification_code",
			"email":     email,
			"error":     err.Error(),
		})
	}

	logrus.Warningf("验证码:%s", verificationCode)
	err = GroupApp.NotificationServicesConfig.SendTestEmail(&model.SendTestEmailReq{
		Email: email,
		Body:  fmt.Sprintf("Your verification code is %s", verificationCode),
	})
	if err != nil {
		return errcode.WithData(200010, map[string]interface{}{ // 新增: 验证码邮件发送失败
			"email": email,
			"error": err.Error(),
		})
	}
	return nil
}

// @description ResetPassword By VerifyCode and Email
func (*User) ResetPassword(ctx context.Context, resetPasswordReq *model.ResetPasswordReq) error {
	if err := utils.ValidatePassword(resetPasswordReq.Password); err != nil {
		return err
	}

	verificationCode, err := global.REDIS.Get(context.Background(), resetPasswordReq.Email+"_code").Result()
	if err != nil {
		return errcode.New(200011) // 验证码已过期
	}
	if verificationCode != resetPasswordReq.VerifyCode {
		return errcode.New(200012) // 验证码错误
	}

	var (
		db   = dal.UserQuery{}
		user = query.User
	)
	info, err := db.First(ctx, user.Email.Eq(resetPasswordReq.Email))
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "query_user",
			"email":     resetPasswordReq.Email,
			"error":     err.Error(),
		})
	}
	t := time.Now().UTC()
	info.PasswordLastUpdated = &t
	info.Password = utils.BcryptHash(resetPasswordReq.Password)
	if err = db.UpdateByEmail(ctx, info, user.Password, user.PasswordLastUpdated); err != nil {
		logrus.Error(ctx, "[ResetPasswordByCode]Update Users info failed:", err)
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "update_password",
			"email":     resetPasswordReq.Email,
			"error":     err.Error(),
		})
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
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "query_user",
			"error":     err.Error(),
		})
	}
	userListRspMap := make(map[string]interface{})
	userListRspMap["total"] = total
	userListRspMap["list"] = list
	return userListRspMap, nil
}

// @description  修改用户信息
func (*User) UpdateUser(updateUserReq *model.UpdateUserReq, claims *utils.UserClaims) error {
	// 密码不能小于6位，如果等于空则不修改密码
	if updateUserReq.Password != nil {
		if len(*updateUserReq.Password) == 0 {
			updateUserReq.Password = nil
		} else if len(*updateUserReq.Password) < 6 {
			return errcode.New(200040) // 密码格式错误
		}
	}

	user, err := dal.GetUsersById(updateUserReq.ID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": updateUserReq.ID,
		})
	}

	// 判断用户权限，租户管理员和租户用户不能修改其他租户的信息
	if claims.Authority == "TENANT_ADMIN" || claims.Authority == "TENANT_USER" {
		if *user.TenantID != claims.TenantID {
			return errcode.New(errcode.CodeNoPermission) // 无访问权限
		}

		// 租户管理员不能修改自己的状态
		if claims.Authority == "TENANT_ADMIN" && *user.Authority == "TENANT_ADMIN" && *user.Status != *updateUserReq.Status {
			if updateUserReq.Status != nil {
				if updateUserReq.Status != nil {
					return errcode.New(errcode.CodeOpDenied) // 操作被拒绝
				}
			}
		}
	}

	t := time.Now().UTC()
	// 密码更新处理
	if updateUserReq.Password != nil {
		hashedPassword := utils.BcryptHash(*updateUserReq.Password)
		if hashedPassword == "" {
			return errcode.New(errcode.CodeDecryptError) // 密码加密失败
		}
		user.Password = hashedPassword
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
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": claims.ID,
		})
	}

	// 修改角色
	if updateUserReq.RoleIDs != nil {
		// 先删除原有角色
		GroupApp.Casbin.RemoveUserAndRole(updateUserReq.ID)
		// 绑定新角色
		if len(updateUserReq.RoleIDs) > 0 {
			ok := GroupApp.Casbin.AddRolesToUser(updateUserReq.ID, updateUserReq.RoleIDs)
			if !ok {
				return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
					"error":    "Failed to update user roles",
					"user_id":  updateUserReq.ID,
					"role_ids": updateUserReq.RoleIDs,
				})
			}
		}
	}

	return nil
}

// @description  删除用户
func (*User) DeleteUser(id string, claims *utils.UserClaims) error {
	// 获取用户信息
	user, err := dal.GetUsersById(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": id,
		})
	}

	// 判断用户权限，租户管理员和租户用户不能修改其他租户的信息
	if claims.Authority == "TENANT_ADMIN" || claims.Authority == "TENANT_USER" {
		// 检查租户权限
		if *user.TenantID != claims.TenantID {
			return errcode.WithVars(errcode.CodeNoPermission, map[string]interface{}{
				"required_tenant": *user.TenantID,
				"current_tenant":  claims.TenantID,
				"operation":       "delete_user",
			})
		}

		// 租户管理员不能删除自己
		// if claims.Authority == "TENANT_ADMIN" && *user.Authority == "TENANT_ADMIN" {
		// 	return errcode.WithVars(errcode.CodeOpDenied, map[string]interface{}{
		// 		"reason":  "cannot_delete_self",
		// 		"user_id": id,
		// 	})
		// }
	}

	// 不能删除系统管理员
	if *user.Authority == "SYS_ADMIN" {
		return errcode.WithVars(errcode.CodeOpDenied, map[string]interface{}{
			"reason":  "cannot_delete_sys_admin",
			"user_id": id,
		})
	}

	// 删除用户
	err = dal.DeleteUsersById(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":     err.Error(),
			"user_id":   id,
			"operation": "delete_user",
		})
	}

	return nil
}

// 获取用户信息
func (*User) GetUser(id string, claims *utils.UserClaims) (*model.User, error) {
	// 获取用户信息
	user, err := dal.GetUsersById(id)
	if err != nil {
		// 数据库错误处理
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": id,
		})
	}

	// 判断用户权限，租户管理员和租户用户不能查看其他租户的信息
	if claims.Authority == "TENANT_ADMIN" || claims.Authority == "TENANT_USER" {
		if *user.TenantID != claims.TenantID {
			return nil, errcode.WithVars(errcode.CodeNoPermission, map[string]interface{}{
				"required_tenant": *user.TenantID,
				"current_tenant":  claims.TenantID,
				"user_authority":  claims.Authority,
			})
		}
	}

	return user, nil
}

// 获取用户详细信息
func (*User) GetUserDetail(claims *utils.UserClaims) (*model.User, error) {
	user, err := dal.GetUsersById(claims.ID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": claims.ID,
		})
	}
	return user, nil
}

// @description 修改用户信息（只能修改自己）
func (*User) UpdateUserInfo(ctx context.Context, updateUserReq *model.UpdateUserInfoReq, claims *utils.UserClaims) error {
	user, err := dal.GetUsersById(claims.ID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": claims.ID,
		})
	}

	// 限制用户只能修改自己的信息
	if user.ID != claims.ID {
		return errcode.WithVars(errcode.CodeNoPermission, map[string]interface{}{
			"reason":  "cannot_update_other_user_info",
			"user_id": claims.ID,
		})
	}

	// 是否加密配置
	if logic.UserIsEncrypt(ctx) {
		password, err := initialize.DecryptPassword(*updateUserReq.Password)
		if err != nil {
			return errcode.WithData(errcode.CodeDecryptError, map[string]interface{}{
				"error": err.Error(),
			})
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
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": claims.ID,
		})
	}
	return err
}

// @description SuperAdmin Become Other admin
func (*User) TransformUser(transformUserReq *model.TransformUserReq, claims *utils.UserClaims) (*model.LoginRsp, error) {
	// 权限检查
	if claims.Authority != "SYS_ADMIN" && claims.Authority != "TENANT_ADMIN" {
		return nil, errcode.WithVars(errcode.CodeNoPermission, map[string]interface{}{
			"required_authority": "SYS_ADMIN or TENANT_ADMIN",
			"current_authority":  claims.Authority,
		})
	}

	// 获取目标用户信息
	becomeUser, err := dal.GetUsersById(transformUserReq.BecomeUserID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": transformUserReq.BecomeUserID,
		})
	}

	// 检查用户状态
	if *becomeUser.Status != "N" {
		return nil, errcode.WithVars(errcode.CodeUserDisabled, map[string]interface{}{
			"user_id":         becomeUser.ID,
			"current_status":  *becomeUser.Status,
			"required_status": "N",
		})
	}

	// 获取JWT密钥
	key := viper.GetString("jwt.key")
	if key == "" {
		return nil, errcode.New(errcode.CodeSystemError)
	}

	// 生成用户Claims
	becomeUserClaims := utils.UserClaims{
		ID:         becomeUser.ID,
		Email:      becomeUser.Email,
		Authority:  *becomeUser.Authority,
		CreateTime: time.Now().UTC(),
		TenantID:   *becomeUser.TenantID,
	}

	// 生成token
	jwt := utils.NewJWT([]byte(key))
	token, err := jwt.GenerateToken(becomeUserClaims)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeTokenGenerateError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": becomeUser.ID,
		})
	}

	// 保存token到Redis
	err = global.REDIS.Set(context.Background(), token, "1", 24*7*time.Hour).Err()
	if err != nil {
		return nil, errcode.WithData(errcode.CodeTokenSaveError, map[string]interface{}{
			"error":   err.Error(),
			"user_id": becomeUser.ID,
		})
	}

	// 返回登录响应
	loginRsp := &model.LoginRsp{
		Token:     &token,
		ExpiresIn: int64(24 * 7 * time.Hour.Seconds()),
	}

	return loginRsp, nil
}

// EmailRegister 邮箱注册
func (u *User) EmailRegister(ctx context.Context, req *model.EmailRegisterReq) (*model.LoginRsp, error) {
	// 密码格式校验
	if err := utils.ValidatePassword(req.Password); err != nil {
		return nil, err
	}

	// 验证码校验
	verificationCode, err := global.REDIS.Get(context.Background(), req.Email+"_code").Result()
	if err != nil {
		return nil, errcode.New(200011) // 验证码已过期
	}
	if verificationCode != req.VerifyCode {
		return nil, errcode.New(200012) // 验证码错误
	}

	// 密码一致性校验
	if req.ConfirmPassword != nil && *req.ConfirmPassword != req.Password {
		return nil, errcode.New(200041) // 两次输入的密码不一致
	}

	// 验证邮箱是否已注册
	user, err := dal.GetUsersByEmail(req.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "query_user",
			"email":     req.Email,
			"error":     err.Error(),
		})
	}
	if user != nil {
		return nil, errcode.New(200008) // 邮箱已注册
	}

	// 密码加密处理
	if logic.UserIsEncrypt(ctx) {
		if req.Salt == nil {
			return nil, errcode.New(200042) // 加密盐值为空
		}
		password, err := initialize.DecryptPassword(req.Password)
		if err != nil {
			return nil, errcode.New(200043) // 密码解密失败
		}
		passwords := strings.TrimSuffix(string(password), *req.Salt)
		req.Password = passwords
	}

	// bcrypt加密密码
	req.Password = utils.BcryptHash(req.Password)

	now := time.Now().UTC()
	tenantID, err := common.GenerateRandomString(8)
	if err != nil {
		logrus.Error("生成租户ID失败", err)
		return nil, errcode.New(errcode.CodeSystemError)
	}

	// 构建用户信息
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
	}

	// 创建用户
	if err = dal.CreateUsers(userInfo); err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "create_user",
			"email":     req.Email,
			"error":     err.Error(),
		})
	}

	// 给租户新增一个默认的首页看板
	err = dal.BoardQuery{}.CreateDefaultBoard(ctx, tenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "create_default_board",
			"tenant_id": tenantID,
			"error":     err.Error(),
		})
	}

	return u.UserLoginAfter(userInfo)
}

// 通过用户手机号获取用户邮箱
func (u *User) GetUserEmailByPhoneNumber(phoneNumber string) (string, error) {
	// 查询用户信息
	user, err := dal.GetUsersByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errcode.New(200007)
		}
		return "", errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"message": "get_user_by_phone_number",
			"error":   err.Error(),
		})
	}
	return user.Email, nil
}

// 根据租户ID查询租户信息
func (u *User) GetTenantInfo(tenantID string) (*model.User, error) {
	tenant, err := dal.GetTenantsById(tenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":     err.Error(),
			"tenant_id": tenantID,
		})
	}
	return tenant, nil
}
