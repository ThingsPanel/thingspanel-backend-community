package dal

import (
	"context"

	global "project/global"
	model "project/internal/model"
	query "project/query"
	utils "project/utils"

	"github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gen"
)

func CreateUiElements(uielements *model.SysUIElement) error {
	return query.SysUIElement.Create(uielements)
}

func UpdateUiElements(uielements *model.SysUIElement) error {
	p := query.SysUIElement
	_, err := query.SysUIElement.Where(p.ID.Eq(uielements.ID)).Updates(uielements)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func DeleteUiElements(id string) error {
	_, err := query.SysUIElement.Where(query.SysUIElement.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetUiElementsListByPage(uielements *model.GetUiElementsListByPageReq) (int64, interface{}, error) {
	q := query.SysUIElement
	var count int64
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.ParentID.Eq("0"))
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}
	if uielements.Page != 0 && uielements.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(uielements.PageSize)
		queryBuilder = queryBuilder.Offset((uielements.Page - 1) * uielements.PageSize)
	}

	uielementsList, err := queryBuilder.Select().Order(q.Order_).Find()
	if err != nil {
		logrus.Error(err)
		return count, uielementsList, err
	}

	uielementsListrsp := []*model.UiElementsListRsp{}
	for i := range uielementsList {
		uielementsListrsp = append(uielementsListrsp, uielementsList[i].ToRsp())
		queryChildren(uielementsListrsp[i])
	}
	return count, uielementsListrsp, err
}

func GetUiElementsListByAuthority(u *utils.UserClaims) (int64, interface{}, error) {
	// 系统管理员和租户管理员菜单树
	if u.Authority == "SYS_ADMIN" || u.Authority == "TENANT_ADMIN" {
		q := query.SysUIElement
		var count int64
		queryBuilder := q.WithContext(context.Background())
		queryBuilder = queryBuilder.Where(gen.Cond(datatypes.JSONQuery("authority").HasKey(u.Authority))...)
		uielementsList, err := queryBuilder.Where(q.ParentID.Eq("0")).Order(q.Order_).Find()
		if err != nil {
			logrus.Error(err)
			return count, uielementsList, err
		}
		count, err = queryBuilder.Count()

		uielementsListrsp := []*model.UiElementsListRsp{}
		for i := range uielementsList {
			uielementsListrsp = append(uielementsListrsp, uielementsList[i].ToRsp())
			queryChildrenByAuthority(uielementsListrsp[i], u.Authority)
		}
		return count, uielementsListrsp, err
	} else {
		// 租户用户菜单树
		// 从casbin_rule表查询当前用户拥有的根权限
		var uielementsList []*model.SysUIElement
		err := global.DB.Raw(`select * from
		(
		select distinct (crp.v1) from casbin_rule crp 
		inner join 
		(
		select cr.v1 from casbin_rule cr  where cr.ptype ='g' and cr.v0 = ? 
		) crr
		 on crr.v1 = crp.v0 where crp.ptype ='p'
		) t
		left join sys_ui_elements tf on t.v1 = tf.id 
		where tf.parent_id ='0' 
		order by tf.orders desc`, u.ID).Scan(&uielementsList)
		if err.Error != nil {
			return 0, nil, err.Error
		}
		data := []*model.UiElementsListRsp{}
		for i := range uielementsList {
			data = append(data, uielementsList[i].ToRsp())
			queryChildrenByUserID(data[i], u.ID)
		}
		return 0, data, nil
	}
}

// 获取租户下权限配置表单树
func GetTenantUiElementsList() (interface{}, error) {
	q := query.SysUIElement
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(gen.Cond(datatypes.JSONQuery("authority").HasKey("TENANT_ADMIN"))...)
	uielementsList, err := queryBuilder.Where(q.ParentID.Eq("0"), q.ElementType.In(1, 2, 3)).Order(q.Order_).Find()
	if err != nil {
		logrus.Error(err)
		return uielementsList, err
	}

	uielementsListrsp := []*model.UiElementsListRsp1{}
	for i := range uielementsList {
		uielementsListrsp = append(uielementsListrsp, uielementsList[i].ToRsp1())
		queryChildrenByAuthority1(uielementsListrsp[i], "TENANT_ADMIN")
	}
	return uielementsListrsp, err
}

func queryChildren(parent *model.UiElementsListRsp) {
	var children []*model.SysUIElement
	children, err := query.SysUIElement.Where(query.SysUIElement.ParentID.Eq(parent.ID)).Order(query.SysUIElement.Order_).Find()
	if err != nil {
		logrus.Error(err)
	}
	if children == nil {
		return
	}
	for i := range children {
		parent.Children = append(parent.Children, children[i].ToRsp())
		queryChildren(parent.Children[i])
	}
}

func queryChildrenByAuthority(parent *model.UiElementsListRsp, authority string) {
	var children []*model.SysUIElement
	children, err := query.SysUIElement.Where(
		query.SysUIElement.ParentID.Eq(parent.ID),
		query.SysUIElement.Where(gen.Cond(datatypes.JSONQuery("authority").HasKey(authority))...),
	).Order(query.SysUIElement.Order_).Find()
	if err != nil {
		logrus.Error(err)
	}
	if children == nil {
		return
	}
	for i := range children {
		parent.Children = append(parent.Children, children[i].ToRsp())
		queryChildrenByAuthority(parent.Children[i], authority)
	}
}
func queryChildrenByAuthority1(parent *model.UiElementsListRsp1, authority string) {
	var children []*model.SysUIElement
	children, err := query.SysUIElement.Where(
		query.SysUIElement.ParentID.Eq(parent.ID),
		query.SysUIElement.ElementType.In(1, 2, 3),
		query.SysUIElement.Where(gen.Cond(datatypes.JSONQuery("authority").HasKey(authority))...),
	).Order(query.SysUIElement.Order_).Find()
	if err != nil {
		logrus.Error(err)
	}
	if children == nil {
		return
	}
	for i := range children {
		parent.Children = append(parent.Children, children[i].ToRsp1())
		queryChildrenByAuthority1(parent.Children[i], authority)
	}
}

func queryChildrenByUserID(parent *model.UiElementsListRsp, userID string) {
	var children []*model.SysUIElement
	err := global.DB.Raw(`select * from
		(
		select distinct (crp.v1) from casbin_rule crp 
		inner join 
		(
		select cr.v1 from casbin_rule cr  where cr.ptype ='g' and cr.v0 = ? 
		) crr
		 on crr.v1 = crp.v0 where crp.ptype ='p'
		) t
		left join sys_ui_elements tf on t.v1 = tf.id 
		where tf.parent_id =? 
		order by tf.orders desc`, userID, parent.ID).Scan(&children)
	if err.Error != nil {
		logrus.Error(err)
	}
	if children == nil {
		return
	}
	for i := range children {
		parent.Children = append(parent.Children, children[i].ToRsp())
		queryChildrenByUserID(parent.Children[i], userID)
	}
}
