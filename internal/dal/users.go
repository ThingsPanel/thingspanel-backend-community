package dal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	common "project/pkg/common"
	global "project/pkg/global"
	utils "project/pkg/utils"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
	"gorm.io/gen/field"
	"gorm.io/gorm"
)

const (
	SYS_ADMIN    = "SYS_ADMIN"
	TENANT_ADMIN = "TENANT_ADMIN"
	TENANT_USER  = "TENANT_USER"
)

func CreateUsers(user *model.User) error {
	return query.User.Create(user)
}

func CreateUserWithAddress(user *model.User, addressReq *model.CreateUserAddressReq) error {
	return query.Q.Transaction(func(tx *query.Query) error {
		// 创建用户
		if err := tx.User.Create(user); err != nil {
			return err
		}

		// 如果提供了地址信息，则创建地址
		if addressReq != nil {
			userAddress := &model.UserAddress{
				UserID:          user.ID,
				Country:         addressReq.Country,
				Province:        addressReq.Province,
				City:            addressReq.City,
				District:        addressReq.District,
				Street:          addressReq.Street,
				DetailedAddress: addressReq.DetailedAddress,
				PostalCode:      addressReq.PostalCode,
				AddressLabel:    addressReq.AddressLabel,
				Longitude:       addressReq.Longitude,
				Latitude:        addressReq.Latitude,
				AdditionalInfo:  addressReq.AdditionalInfo,
			}

			if err := tx.UserAddress.Create(userAddress); err != nil {
				return err
			}
		}

		return nil
	})
}

func UpdateUserWithAddress(user *model.User, addressReq *model.UpdateUserAddressReq) error {
	return query.Q.Transaction(func(tx *query.Query) error {
		// 更新用户信息
		if _, err := tx.User.Where(tx.User.ID.Eq(user.ID)).Updates(user); err != nil {
			return err
		}

		// 处理地址信息
		if addressReq != nil {
			// 查找现有地址
			existingAddress, err := tx.UserAddress.Where(tx.UserAddress.UserID.Eq(user.ID)).First()
			if err != nil {
				// 如果地址不存在，创建新地址
				if errors.Is(err, gorm.ErrRecordNotFound) {
					newAddress := &model.UserAddress{
						UserID:          user.ID,
						Country:         addressReq.Country,
						Province:        addressReq.Province,
						City:            addressReq.City,
						District:        addressReq.District,
						Street:          addressReq.Street,
						DetailedAddress: addressReq.DetailedAddress,
						PostalCode:      addressReq.PostalCode,
						AddressLabel:    addressReq.AddressLabel,
						Longitude:       addressReq.Longitude,
						Latitude:        addressReq.Latitude,
						AdditionalInfo:  addressReq.AdditionalInfo,
					}
					if err := tx.UserAddress.Create(newAddress); err != nil {
						return err
					}
				} else {
					return err
				}
			} else {
				// 更新现有地址
				updates := map[string]interface{}{}
				if addressReq.Country != nil {
					updates["country"] = *addressReq.Country
				}
				if addressReq.Province != nil {
					updates["province"] = *addressReq.Province
				}
				if addressReq.City != nil {
					updates["city"] = *addressReq.City
				}
				if addressReq.District != nil {
					updates["district"] = *addressReq.District
				}
				if addressReq.Street != nil {
					updates["street"] = *addressReq.Street
				}
				if addressReq.DetailedAddress != nil {
					updates["detailed_address"] = *addressReq.DetailedAddress
				}
				if addressReq.PostalCode != nil {
					updates["postal_code"] = *addressReq.PostalCode
				}
				if addressReq.AddressLabel != nil {
					updates["address_label"] = *addressReq.AddressLabel
				}
				if addressReq.Longitude != nil {
					updates["longitude"] = *addressReq.Longitude
				}
				if addressReq.Latitude != nil {
					updates["latitude"] = *addressReq.Latitude
				}
				if addressReq.AdditionalInfo != nil {
					updates["additional_info"] = *addressReq.AdditionalInfo
				}

				if len(updates) > 0 {
					if _, err := tx.UserAddress.Where(tx.UserAddress.ID.Eq(existingAddress.ID)).Updates(updates); err != nil {
						return err
					}
				}
			}
		}

		return nil
	})
}

func GetUsersById(uid string) (*model.User, error) {
	user, err := query.User.Where(query.User.ID.Eq(uid)).First()
	if err != nil {
		return nil, err
	}
	return user, err
}

func GetUserByIdWithAddress(uid string) (map[string]interface{}, error) {
	q := query.User
	qa := query.UserAddress

	// 联表查询用户和地址信息
	type UserWithAddress struct {
		// 用户字段
		ID                  string     `gorm:"column:id"`
		Name                *string    `gorm:"column:name"`
		PhoneNumber         string     `gorm:"column:phone_number"`
		Email               string     `gorm:"column:email"`
		Status              *string    `gorm:"column:status"`
		Authority           *string    `gorm:"column:authority"`
		TenantID            *string    `gorm:"column:tenant_id"`
		Remark              *string    `gorm:"column:remark"`
		AdditionalInfo      *string    `gorm:"column:additional_info"`
		Organization        *string    `gorm:"column:organization"`
		Timezone            *string    `gorm:"column:timezone"`
		DefaultLanguage     *string    `gorm:"column:default_language"`
		CreatedAt           *time.Time `gorm:"column:created_at"`
		UpdatedAt           *time.Time `gorm:"column:updated_at"`
		PasswordLastUpdated *time.Time `gorm:"column:password_last_updated"`
		LastVisitTime       *time.Time `gorm:"column:last_visit_time"`
		LastVisitIP         *string    `gorm:"column:last_visit_ip"`
		LastVisitDevice     *string    `gorm:"column:last_visit_device"`
		PasswordFailCount   *int32     `gorm:"column:password_fail_count"`
		AvatarURL           *string    `gorm:"column:avatar_url"`
		// 地址字段
		AddressID             *int32     `gorm:"column:address_id"`
		Country               *string    `gorm:"column:address_country"`
		Province              *string    `gorm:"column:address_province"`
		City                  *string    `gorm:"column:address_city"`
		District              *string    `gorm:"column:address_district"`
		Street                *string    `gorm:"column:address_street"`
		DetailedAddress       *string    `gorm:"column:address_detailed_address"`
		PostalCode            *string    `gorm:"column:address_postal_code"`
		AddressLabel          *string    `gorm:"column:address_label"`
		Longitude             *string    `gorm:"column:address_longitude"`
		Latitude              *string    `gorm:"column:address_latitude"`
		AddressAdditionalInfo *string    `gorm:"column:address_additional_info"`
		AddressCreatedTime    *time.Time `gorm:"column:address_created_time"`
		AddressUpdatedTime    *time.Time `gorm:"column:address_updated_time"`
	}

	var result UserWithAddress
	err := q.WithContext(context.Background()).
		LeftJoin(qa, q.ID.EqCol(qa.UserID)).
		Where(q.ID.Eq(uid)).
		Select(
			q.ID, q.Name, q.PhoneNumber, q.Email, q.Status, q.Authority, q.TenantID, q.Remark,
			q.AdditionalInfo, q.Organization, q.Timezone, q.DefaultLanguage,
			q.CreatedAt, q.UpdatedAt, q.PasswordLastUpdated, q.LastVisitTime, q.LastVisitIP, q.LastVisitDevice, q.PasswordFailCount, q.AvatarURL,
			qa.ID.As("address_id"),
			qa.Country.As("address_country"), qa.Province.As("address_province"), qa.City.As("address_city"),
			qa.District.As("address_district"), qa.Street.As("address_street"),
			qa.DetailedAddress.As("address_detailed_address"), qa.PostalCode.As("address_postal_code"),
			qa.AddressLabel.As("address_label"), qa.Longitude.As("address_longitude"),
			qa.Latitude.As("address_latitude"), qa.AdditionalInfo.As("address_additional_info"),
			qa.CreatedTime.As("address_created_time"), qa.UpdatedTime.As("address_updated_time"),
		).
		Scan(&result)

	if err != nil {
		return nil, err
	}

	// 如果没有找到用户记录（ID为空），返回记录不存在错误
	if result.ID == "" {
		return nil, gorm.ErrRecordNotFound
	}

	// 获取用户角色
	roles, _ := GetRolesByUserId(result.ID)

	// 构建返回数据
	userMap := map[string]interface{}{
		"id":                    result.ID,
		"name":                  result.Name,
		"phone_number":          result.PhoneNumber,
		"email":                 result.Email,
		"status":                result.Status,
		"authority":             result.Authority,
		"tenant_id":             result.TenantID,
		"remark":                result.Remark,
		"additional_info":       result.AdditionalInfo,
		"organization":          result.Organization,
		"timezone":              result.Timezone,
		"default_language":      result.DefaultLanguage,
		"created_at":            result.CreatedAt,
		"updated_at":            result.UpdatedAt,
		"password_last_updated": result.PasswordLastUpdated,
		"last_visit_time":       result.LastVisitTime,
		"last_visit_ip":         result.LastVisitIP,
		"last_visit_device":     result.LastVisitDevice,
		"password_fail_count":   result.PasswordFailCount,
		"avatar_url":            result.AvatarURL,
		"user_roles":            roles,
	}

	// 添加地址信息（如果存在）
	if result.AddressID != nil {
		userMap["address"] = map[string]interface{}{
			"id":               result.AddressID,
			"country":          result.Country,
			"province":         result.Province,
			"city":             result.City,
			"district":         result.District,
			"street":           result.Street,
			"detailed_address": result.DetailedAddress,
			"postal_code":      result.PostalCode,
			"address_label":    result.AddressLabel,
			"longitude":        result.Longitude,
			"latitude":         result.Latitude,
			"additional_info":  result.AddressAdditionalInfo,
			"created_time":     result.AddressCreatedTime,
			"updated_time":     result.AddressUpdatedTime,
		}
	} else {
		userMap["address"] = nil
	}

	return userMap, nil
}

func GetUsersByEmail(email string) (*model.User, error) {
	q := query.User
	user, err := q.Where(q.Email.Eq(email)).First()
	if err != nil {
		return nil, err
	}
	return user, err
}

// 通过手机号获取用户
// 支持国际手机号匹配：
// - 如果输入带区号(+XX NNNN)：精确匹配
// - 如果输入不带区号(纯数字)：模糊匹配数字后缀(LIKE '%digits')
func GetUsersByPhoneNumber(phoneNumber string) (*model.User, error) {
	if phoneNumber == "" {
		return nil, errors.New("phone number is empty")
	}

	q := query.User

	// 判断输入是否带区号
	hasCountryCode := strings.HasPrefix(phoneNumber, "+")

	if hasCountryCode {
		// 输入带区号：直接精确匹配（输入格式规范：+86 13100000000）
		user, err := q.Where(q.PhoneNumber.Eq(phoneNumber)).First()
		return user, err
	} else {
		// 输入不带区号：使用 LIKE 匹配数字后缀
		// 例如：输入 13100000000，可匹配 +86 13100000000 或 13100000000
		user, err := q.Where(q.PhoneNumber.Like("%" + phoneNumber)).First()
		return user, err
	}
}

func GetUserListByPage(userListReq *model.UserListReq, claims *utils.UserClaims) (int64, interface{}, error) {
	return GetUserListByPageWithAddress(userListReq, claims)
}

func GetUserListByPageWithAddress(userListReq *model.UserListReq, claims *utils.UserClaims) (int64, interface{}, error) {
	q := query.User
	qa := query.UserAddress
	var count int64
	var userList []map[string]interface{}

	queryBuilder := q.WithContext(context.Background()).LeftJoin(qa, q.ID.EqCol(qa.UserID))

	// 权限过滤
	if claims.Authority == TENANT_ADMIN || claims.Authority == TENANT_USER {
		queryBuilder = queryBuilder.Where(q.TenantID.Eq(claims.TenantID))
		queryBuilder = queryBuilder.Where(q.Authority.Eq(TENANT_USER))
	} else if claims.Authority == SYS_ADMIN {
		queryBuilder = queryBuilder.Where(q.Authority.Eq(TENANT_ADMIN))
	} else {
		return count, nil, fmt.Errorf("authority exception")
	}

	// 用户基本信息过滤
	if userListReq.Email != nil && *userListReq.Email != "" {
		queryBuilder = queryBuilder.Where(q.Email.Like(fmt.Sprintf("%%%s%%", *userListReq.Email)))
	}
	if userListReq.PhoneNumber != nil && *userListReq.PhoneNumber != "" {
		queryBuilder = queryBuilder.Where(q.PhoneNumber.Eq(*userListReq.PhoneNumber))
	}
	if userListReq.Name != nil && *userListReq.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *userListReq.Name)))
	}
	if userListReq.Status != nil && *userListReq.Status != "" {
		queryBuilder = queryBuilder.Where(q.Status.Eq(*userListReq.Status))
	}

	// 新增扩展字段过滤
	if userListReq.Organization != nil && *userListReq.Organization != "" {
		queryBuilder = queryBuilder.Where(q.Organization.Like(fmt.Sprintf("%%%s%%", *userListReq.Organization)))
	}

	// 地址相关过滤
	if userListReq.Country != nil && *userListReq.Country != "" {
		queryBuilder = queryBuilder.Where(qa.Country.Like(fmt.Sprintf("%%%s%%", *userListReq.Country)))
	}
	if userListReq.Province != nil && *userListReq.Province != "" {
		queryBuilder = queryBuilder.Where(qa.Province.Like(fmt.Sprintf("%%%s%%", *userListReq.Province)))
	}
	if userListReq.City != nil && *userListReq.City != "" {
		queryBuilder = queryBuilder.Where(qa.City.Like(fmt.Sprintf("%%%s%%", *userListReq.City)))
	}

	// 获取总数（1:1关系不需要去重）
	count, err := queryBuilder.Count()
	if err != nil {
		return count, nil, err
	}

	// 分页
	if userListReq.Page != 0 && userListReq.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(userListReq.PageSize)
		queryBuilder = queryBuilder.Offset((userListReq.Page - 1) * userListReq.PageSize)
	}

	// 由于1:1关系不需要去重，直接查询用户ID列表，然后逐个查询详细信息
	type UserIDWithTime struct {
		ID        string     `gorm:"column:id"`
		CreatedAt *time.Time `gorm:"column:created_at"`
	}
	var userIDResults []UserIDWithTime
	err = queryBuilder.Select(q.ID, q.CreatedAt).Order(q.CreatedAt.Desc()).Scan(&userIDResults)
	if err != nil {
		return count, nil, err
	}

	// 为每个用户获取详细信息和地址
	for _, userIDResult := range userIDResults {
		userWithAddress, err := GetUserByIdWithAddress(userIDResult.ID)
		if err != nil {
			logrus.Error("Failed to get user with address for ID:", userIDResult.ID, err)
			continue
		}
		userList = append(userList, userWithAddress)
	}

	return count, userList, nil
}

func UpdateUserAddressOnly(userID string, addressReq *model.UpdateUserAddressReq) error {
	return query.Q.Transaction(func(tx *query.Query) error {
		// 查找现有地址
		existingAddress, err := tx.UserAddress.Where(tx.UserAddress.UserID.Eq(userID)).First()
		if err != nil {
			// 如果地址不存在，创建新地址
			if errors.Is(err, gorm.ErrRecordNotFound) {
				newAddress := &model.UserAddress{
					UserID:          userID,
					Country:         addressReq.Country,
					Province:        addressReq.Province,
					City:            addressReq.City,
					District:        addressReq.District,
					Street:          addressReq.Street,
					DetailedAddress: addressReq.DetailedAddress,
					PostalCode:      addressReq.PostalCode,
					AddressLabel:    addressReq.AddressLabel,
					Longitude:       addressReq.Longitude,
					Latitude:        addressReq.Latitude,
					AdditionalInfo:  addressReq.AdditionalInfo,
				}
				return tx.UserAddress.Create(newAddress)
			} else {
				return err
			}
		} else {
			// 更新现有地址
			updates := map[string]interface{}{}
			if addressReq.Country != nil {
				updates["country"] = *addressReq.Country
			}
			if addressReq.Province != nil {
				updates["province"] = *addressReq.Province
			}
			if addressReq.City != nil {
				updates["city"] = *addressReq.City
			}
			if addressReq.District != nil {
				updates["district"] = *addressReq.District
			}
			if addressReq.Street != nil {
				updates["street"] = *addressReq.Street
			}
			if addressReq.DetailedAddress != nil {
				updates["detailed_address"] = *addressReq.DetailedAddress
			}
			if addressReq.PostalCode != nil {
				updates["postal_code"] = *addressReq.PostalCode
			}
			if addressReq.AddressLabel != nil {
				updates["address_label"] = *addressReq.AddressLabel
			}
			if addressReq.Longitude != nil {
				updates["longitude"] = *addressReq.Longitude
			}
			if addressReq.Latitude != nil {
				updates["latitude"] = *addressReq.Latitude
			}
			if addressReq.AdditionalInfo != nil {
				updates["additional_info"] = *addressReq.AdditionalInfo
			}

			if len(updates) > 0 {
				_, err := tx.UserAddress.Where(tx.UserAddress.ID.Eq(existingAddress.ID)).Updates(updates)
				return err
			}
		}

		return nil
	})
}

// 多余
func UpdateUserInfoByIdPersonal(uid string, data *model.UpdateUserInfoReq) (int64, error) {
	q := query.User
	t := time.Now()
	data.UpdatedAt = &t
	r, err := query.User.Where(q.ID.Eq(uid)).Updates(data)
	return r.RowsAffected, err
}

func UpdateUserInfoById(_ string, data *model.User) (int64, error) {
	q := query.User
	r, err := query.User.Where(q.ID.Eq(data.ID)).Updates(data)
	return r.RowsAffected, err
}

func DeleteUsersById(uid string) error {
	_, err := query.User.Where(query.User.ID.Eq(uid)).Delete()
	return err
}

func GetUserIdBYTenantID(tenantID string) (string, error) {
	var (
		userId     string
		cacheKeyId = fmt.Sprintf("GetUserIdBYTenantID:%s", tenantID)
		err        error
	)
	userId, err = global.REDIS.Get(context.Background(), cacheKeyId).Result()
	if err == nil {
		return userId, nil
	}
	err = query.User.Where(query.User.TenantID.Eq(tenantID)).Select(query.User.ID).Scan(&userId)
	if err != nil {
		return userId, err
	}
	global.REDIS.Set(context.Background(), cacheKeyId, userId, time.Hour*6)
	return userId, nil
}

type UserQuery struct {
}

func (UserQuery) Count(ctx context.Context) (count int64, err error) {
	count, err = query.User.Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (UserQuery) CountByWhere(ctx context.Context, option ...gen.Condition) (count int64, err error) {
	var users = query.User
	count, err = users.Where(option...).Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (UserQuery) GroupByMonthCount(ctx context.Context, email *string) (list []*model.GetBoardUserListMonth) {
	var (
		db = global.DB.WithContext(ctx)
	)
	conn := db.Model(&model.User{}).Select("(EXTRACT(MONTH FROM created_at) ) AS mon,COUNT(1) as num").
		Where("created_at > ? and created_at  IS NOT NULL", common.GetYearStart()).
		Group("EXTRACT(MONTH FROM created_at)").Order("mon")

	if email != nil {
		conn = conn.Where("email = ?", *email)
	}

	conn.Scan(&list)

	return
}

func (UserQuery) First(ctx context.Context, option ...gen.Condition) (info *model.User, err error) {
	var users = query.User

	info, err = users.Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (UserQuery) Select(ctx context.Context, option ...gen.Condition) (list []*model.User, err error) {
	var users = query.User

	list, err = users.Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (UserQuery) UpdateByEmail(ctx context.Context, info *model.User, columns ...field.Expr) (err error) {
	var users = query.User
	//users.Password, users.Name, users.PhoneNumber, users.Remark
	_, err = users.Where(users.Email.Eq(info.Email)).
		Select(columns...).
		UpdateColumns(info)
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// 更新上次登录时间
func (UserQuery) UpdateLastVisitTime(ctx context.Context, uid string) (err error) {
	_, err = query.User.Where(query.User.ID.Eq(uid)).Update(query.User.LastVisitTime, time.Now())
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

type UserVo struct {
}

func (UserVo) PoToVo(userInfo *model.User) (info *model.UsersRes) {
	info = &model.UsersRes{
		ID:       userInfo.ID,
		PhoneNum: userInfo.PhoneNumber,
		Email:    userInfo.Email,
	}
	if userInfo.Name != nil {
		info.Name = *userInfo.Name
	}
	if userInfo.Authority != nil {
		info.Authority = *userInfo.Authority
	}
	if userInfo.TenantID != nil {
		info.TenantID = *userInfo.TenantID
	}
	if userInfo.Remark != nil {
		info.Remark = *userInfo.Remark
	}
	if userInfo.CreatedAt != nil {
		info.CreateTime = common.DateTimeToString(*userInfo.CreatedAt, "")
	}
	if userInfo.AdditionalInfo != nil {
		info.AdditionalInfo = *userInfo.AdditionalInfo
	}
	if userInfo.AvatarURL != nil {
		info.AvatarURL = *userInfo.AvatarURL
	}
	return
}

// 查询租户管理员列表
func (UserVo) GetTenantAdminList() (list []*model.User, err error) {
	var users = query.User
	userInfoList, err := users.Where(users.Authority.Eq(TENANT_ADMIN)).Find()
	if err != nil {
		logrus.Error(err)
		return
	}
	return userInfoList, nil
}

// 根据租户ID查询租户信息
func GetTenantsById(tenantID string) (info *model.User, err error) {
	var tenants = query.User
	info, err = tenants.Where(tenants.TenantID.Eq(tenantID), tenants.Authority.Eq(TENANT_ADMIN)).First()
	if err != nil {
		logrus.Error(err)
		return
	}
	return info, nil
}

func CheckPhoneNumberExists(phoneNumber string, excludeUserID ...string) (bool, error) {
	if phoneNumber == "" {
		return false, nil
	}

	// 直接查找这个手机号的用户
	user, err := GetUsersByPhoneNumber(phoneNumber)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil // 没找到，说明不重复
		}
		return false, err
	}

	// 如果找到了，检查是不是要排除的用户（通常是当前用户）
	if len(excludeUserID) > 0 && excludeUserID[0] != "" && user.ID == excludeUserID[0] {
		return false, nil // 是自己的手机号，不算重复
	}

	return true, nil // 是别人的手机号，算重复
}

// GetTenantAdmin 获取租户管理员
func GetTenantAdmin(tenantID string) (*model.User, error) {
	q := query.User
	return q.Where(q.TenantID.Eq(tenantID)).
		Where(q.Authority.Eq(TENANT_ADMIN)).
		First()
}

// GetUserSelector 获取用户选择器列表（租户管理员 + 租户用户）
func GetUserSelector(req *model.UserSelectorReq, tenantID string) (int64, []model.UserSelectorItem, error) {
	q := query.User
	var count int64
	var userList []model.UserSelectorItem

	// 查询租户管理员和普通用户
	queryBuilder := q.WithContext(context.Background()).
		Where(q.TenantID.Eq(tenantID)).
		Where(q.Authority.In(TENANT_ADMIN, TENANT_USER)).
		Where(q.Status.Eq("N")) // 只查询正常状态的用户

	// 名称模糊匹配
	if req.Name != nil && *req.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}

	// 计算总数
	count, err := queryBuilder.Count()
	if err != nil {
		return 0, nil, err
	}

	// 分页查询，按名称正序排列
	offset := (req.Page - 1) * req.PageSize
	err = queryBuilder.Select(
		q.ID.As("user_id"),
		q.Name,
		q.Email,
		q.Authority.As("user_type"),
	).Order(q.Name).Limit(req.PageSize).Offset(offset).Scan(&userList)

	if err != nil {
		return 0, nil, err
	}

	return count, userList, nil
}