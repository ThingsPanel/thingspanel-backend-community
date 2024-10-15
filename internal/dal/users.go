package dal

import (
	"context"
	"fmt"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	common "project/pkg/common"
	global "project/pkg/global"
	utils "project/pkg/utils"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
	"gorm.io/gen/field"
)

const (
	SYS_ADMIN    = "SYS_ADMIN"
	TENANT_ADMIN = "TENANT_ADMIN"
	TENANT_USER  = "TENANT_USER"
)

func CreateUsers(user *model.User) error {
	return query.User.Create(user)
}

func GetUsersById(uid string) (*model.User, error) {
	user, err := query.User.Where(query.User.ID.Eq(uid)).First()
	if err != nil {
		return nil, err
	}
	return user, err
}

func GetUsersByEmail(email string) (*model.User, error) {
	q := query.User
	user, err := q.Where(q.Email.Eq(email)).First()
	if err != nil {
		return nil, err
	}
	return user, err
}

func GetUserListByPage(userListReq *model.UserListReq, claims *utils.UserClaims) (int64, interface{}, error) {
	q := query.User
	var count int64
	var userList []map[string]interface{}
	queryBuilder := q.WithContext(context.Background())

	if claims.Authority == TENANT_ADMIN || claims.Authority == TENANT_USER {
		queryBuilder = queryBuilder.Where(q.TenantID.Eq(claims.TenantID))
		queryBuilder = queryBuilder.Where(q.Authority.Eq(TENANT_USER))
	} else if claims.Authority == SYS_ADMIN {
		queryBuilder = queryBuilder.Where(q.Authority.Eq(TENANT_ADMIN))
	} else {
		return count, nil, fmt.Errorf("authority exception")
	}

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
	count, err := queryBuilder.Count()
	if err != nil {
		return count, nil, err
	}
	if userListReq.Page != 0 && userListReq.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(userListReq.PageSize)
		queryBuilder = queryBuilder.Offset((userListReq.Page - 1) * userListReq.PageSize)
	}

	users, err := queryBuilder.Select(q.ID, q.Name, q.PhoneNumber, q.Email, q.Status, q.Authority, q.TenantID, q.Remark, q.AdditionalInfo, q.CreatedAt, q.UpdatedAt).Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		return count, users, err
	}
	for _, user := range users {
		roles, _ := GetRolesByUserId(user.ID)
		userMap := map[string]interface{}{
			"id":             user.ID,
			"name":           user.Name,
			"phone_number":   user.PhoneNumber,
			"email":          user.Email,
			"status":         user.Status,
			"authority":      user.Authority,
			"tenant_id":      user.TenantID,
			"remark":         user.Remark,
			"additionalInfo": user.AdditionalInfo,
			"created_at":     user.CreatedAt,
			"updated_at":     user.UpdatedAt,
			"userRoles":      roles,
		}
		userList = append(userList, userMap)
	}

	return count, userList, err
}

// 多余
func UpdateUserInfoByIdPersonal(uid string, data *model.UpdateUserInfoReq) (int64, error) {
	q := query.User
	t := time.Now()
	data.UpdatedAt = &t
	r, err := query.User.Where(q.ID.Eq(uid)).Updates(data)
	return r.RowsAffected, err
}

func UpdateUserInfoById(uid string, data *model.User) (int64, error) {
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
	userId, err = global.REDIS.Get(cacheKeyId).Result()
	if err == nil {
		return userId, nil
	}
	err = query.User.Where(query.User.TenantID.Eq(tenantID)).Select(query.User.ID).Scan(&userId)
	if err != nil {
		return userId, err
	}
	global.REDIS.Set(cacheKeyId, userId, time.Hour*6)
	return userId, nil
}

type UserQuery struct {
}

func (u UserQuery) Count(ctx context.Context) (count int64, err error) {
	count, err = query.User.Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (u UserQuery) CountByWhere(ctx context.Context, option ...gen.Condition) (count int64, err error) {
	var users = query.User
	count, err = users.Where(option...).Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (u UserQuery) GroupByMonthCount(ctx context.Context, email *string) (list []*model.GetBoardUserListMonth) {
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

func (u UserQuery) First(ctx context.Context, option ...gen.Condition) (info *model.User, err error) {
	var users = query.User

	info, err = users.Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (u UserQuery) Select(ctx context.Context, option ...gen.Condition) (list []*model.User, err error) {
	var users = query.User

	list, err = users.Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (u UserQuery) UpdateByEmail(ctx context.Context, info *model.User, columns ...field.Expr) (err error) {
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

type UserVo struct {
}

func (u UserVo) PoToVo(userInfo *model.User) (info *model.UsersRes) {
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
	return
}
