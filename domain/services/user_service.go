package services

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"

	"github.com/ThingsPanel/ThingsPanel-Go/global"
	"github.com/ThingsPanel/ThingsPanel-Go/models"
	"github.com/ThingsPanel/ThingsPanel-Go/utils"
	"github.com/ThingsPanel/ThingsPanel-Go/utils/page"
	"github.com/beego/beego/v2/client/orm"
	"github.com/beego/beego/v2/server/web/context"
)

// UserService struct
type UserService struct {
	BaseService
}

// GetUserById 根据id获取一条admin_user数据
func (*UserService) GetUserById(id int) *models.User {
	o := orm.NewOrm()
	user := models.User{Id: id}
	err := o.Read(&user)
	if err != nil {
		return nil
	}
	return &user
}

// AuthCheck 权限检测
func (*UserService) AuthCheck(url string, authExcept map[string]interface{}, loginUser *models.User) bool {
	authURL := loginUser.GetAuthUrl()
	if utils.KeyInMap(url, authExcept) || utils.KeyInMap(url, authURL) {
		return true
	}
	return false
}

// CheckLogin 用户登录验证
func (*UserService) CheckLogin(loginForm formvalidate.LoginForm, ctx *context.Context) (*models.User, error) {
	var user models.User
	o := orm.NewOrm()
	err := o.QueryTable(new(models.User)).Filter("username", loginForm.Username).Limit(1).One(&user)
	if err != nil {
		return nil, errors.New("用户不存在")
	}

	decodePasswdStr, err := base64.StdEncoding.DecodeString(user.Password)

	if err != nil || !utils.PasswordVerify(loginForm.Password, string(decodePasswdStr)) {
		return nil, errors.New("密码错误")
	}

	if user.Status != 1 {
		return nil, errors.New("用户被冻结")
	}

	ctx.Output.Session(global.LOGIN_USER, user)

	if loginForm.Remember != "" {
		ctx.SetCookie(global.LOGIN_USER_ID, strconv.Itoa(user.Id), 7200)
		ctx.SetCookie(global.LOGIN_USER_TOKEN, user.GetTokenStrByUser(ctx), 7200)
	} else {
		ctx.SetCookie(global.LOGIN_USER_ID, ctx.GetCookie(global.LOGIN_USER_ID), -1)
		ctx.SetCookie(global.LOGIN_USER_TOKEN, ctx.GetCookie(global.LOGIN_USER_TOKEN), -1)
	}

	return &user, nil

}

// GetCount 获取user 总数
func (*UserService) GetCount() int {
	count, err := orm.NewOrm().QueryTable(new(models.User)).Count()
	if err != nil {
		return 0
	}
	return int(count)
}

// GetAllUser 获取所有user
func (*UserService) GetAllUser() []*models.User {
	var user []*models.User
	o := orm.NewOrm().QueryTable(new(models.User))
	_, err := o.All(&User)
	if err != nil {
		return nil
	}
	return user
}

// UpdateNickName 系统管理-个人资料-修改昵称
func (*UserService) UpdateNickName(id int, nickname string) int {
	num, err := orm.NewOrm().QueryTable(new(models.User)).Filter("id", id).Update(orm.Params{
		"nickname": nickname,
	})
	if err != nil || num <= 0 {
		return 0
	}
	return int(num)
}

// UpdatePassword 修改密码
func (*UserService) UpdatePassword(id int, newPassword string) int {
	newPasswordForHash, err := utils.PasswordHash(newPassword)

	if err != nil {
		return 0
	}

	num, err := orm.NewOrm().QueryTable(new(models.User)).Filter("id", id).Update(orm.Params{
		"password": base64.StdEncoding.EncodeToString([]byte(newPasswordForHash)),
	})

	if err != nil || num <= 0 {
		return 0
	}

	return int(num)
}

// UpdateAvatar 系统管理-个人资料-修改头像
func (*UserService) UpdateAvatar(id int, avatar string) int {
	num, err := orm.NewOrm().QueryTable(new(models.User)).Filter("id", id).Update(orm.Params{
		"avatar": avatar,
	})
	if err != nil || num <= 0 {
		return 0
	}
	return int(num)
}

// GetPaginateData 通过分页获取user
func (aus *UserService) GetPaginateData(listRows int, params url.Values) ([]*models.User, page.Pagination) {
	//搜索、查询字段赋值
	aus.SearchField = append(aus.SearchField, new(models.User).SearchField()...)

	var user []*models.User
	o := orm.NewOrm().QueryTable(new(models.User))
	_, err := aus.PaginateAndScopeWhere(o, listRows, params).All(&user)
	if err != nil {
		return nil, aus.Pagination
	}
	return user, aus.Pagination
}

// IsExistName 名称验重
func (*UserService) IsExistName(username string, id int) bool {
	if id == 0 {
		return orm.NewOrm().QueryTable(new(models.User)).Filter("username", username).Exist()
	}
	return orm.NewOrm().QueryTable(new(models.User)).Filter("username", username).Exclude("id", id).Exist()
}

// Create 新增admin user用户
func (*UserService) Create(form *formvalidate.UserForm) int {
	newPasswordForHash, err := utils.PasswordHash(form.Password)
	if err != nil {
		return 0
	}

	user := models.User{
		Username: form.Username,
		Password: base64.StdEncoding.EncodeToString([]byte(newPasswordForHash)),
		Nickname: form.Nickname,
		Avatar:   form.Avatar,
		Role:     form.Role,
		Status:   int8(form.Status),
	}
	id, err := orm.NewOrm().Insert(&user)

	if err == nil {
		return int(id)
	}
	return 0
}

// Update 更新用户信息
func (*UserService) Update(form *formvalidate.UserForm) int {
	o := orm.NewOrm()
	user := models.User{Id: form.Id}
	if o.Read(&user) == nil {
		user.Username = form.Username
		user.Nickname = form.Nickname
		user.Role = form.Role
		user.Status = int8(form.Status)
		if user.Password != form.Password {
			newPasswordForHash, err := utils.PasswordHash(form.Password)
			if err == nil {
				user.Password = base64.StdEncoding.EncodeToString([]byte(newPasswordForHash))
			}
		}
		num, err := o.Update(&user)
		if err == nil {
			return int(num)
		}
		return 0
	}
	return 0
}

// Enable 启用用户
func (*UserService) Enable(ids []int) int {
	num, err := orm.NewOrm().QueryTable(new(models.User)).Filter("id__in", ids).Update(orm.Params{
		"status": 1,
	})
	if err == nil {
		return int(num)
	}
	return 0
}

// Disable 禁用用户
func (*UserService) Disable(ids []int) int {
	num, err := orm.NewOrm().QueryTable(new(models.User)).Filter("id__in", ids).Update(orm.Params{
		"status": 0,
	})
	if err == nil {
		return int(num)
	}
	return 0
}

// Del 删除用户
func (*UserService) Del(ids []int) int {
	count, err := orm.NewOrm().QueryTable(new(models.User)).Filter("id__in", ids).Delete()
	if err == nil {
		return int(count)
	}
	return 0
}
