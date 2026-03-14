package dal

import (
	"context"
	"fmt"
	"sort"
	"strings"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"
	utils "project/pkg/utils"

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

func ServeUiElementsListByPage(uielements *model.ServeUiElementsListByPageReq) (int64, interface{}, error) {
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

	var uielementsListrsp []*model.UiElementsListRsp
	for i := range uielementsList {
		uielementsListrsp = append(uielementsListrsp, uielementsList[i].ToRsp())
		queryChildren(uielementsListrsp[i])
	}
	return count, uielementsListrsp, err
}

func ServeUiElementsListByAuthority(u *utils.UserClaims) (int64, interface{}, error) {
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

		var uielementsListrsp []*model.UiElementsListRsp
		for i := range uielementsList {
			uielementsListrsp = append(uielementsListrsp, uielementsList[i].ToRsp())
			queryChildrenByAuthority(uielementsListrsp[i], u.Authority)
		}
		appendTenantDashboardMenus(uielementsListrsp, u.TenantID)
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
		var data []*model.UiElementsListRsp
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

	var uielementsListrsp []*model.UiElementsListRsp1
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

func appendTenantDashboardMenus(roots []*model.UiElementsListRsp, tenantID string) {
	if tenantID == "" {
		return
	}

	menus, err := ListTenantDashboardMenus(tenantID, "home")
	if err != nil || len(menus) == 0 {
		return
	}

	homeNode := findUiElementByCode(roots, "home")
	if homeNode == nil {
		return
	}

	for _, menu := range menus {
		order := menu.Sort
		path := fmt.Sprintf("/home/dashboard/%s", menu.DashboardID)
		icon := "mdi:view-dashboard-outline"
		hideInMenu := "0"
		routePath := "view.visualization_thingsvis-menu-dashboard"
		description := menu.MenuName
		remark := fmt.Sprintf("thingsvis-dashboard:%s", menu.DashboardID)
		authority := `["SYS_ADMIN","TENANT_ADMIN"]`
		child := &model.UiElementsListRsp{
			ID:           menu.ID,
			ParentID:     homeNode.ID,
			ElementCode:  buildDashboardMenuRouteCode(menu.DashboardID),
			ElementType:  int16Ptr(3),
			Orders:       &order,
			Param1:       &path,
			Param2:       &icon,
			Param3:       &hideInMenu,
			Authority:    authority,
			Description:  &description,
			Remark:       &remark,
			Multilingual: nil,
			RoutePath:    &routePath,
			Children:     []*model.UiElementsListRsp{},
		}
		homeNode.Children = append(homeNode.Children, child)
	}

	sort.SliceStable(homeNode.Children, func(i, j int) bool {
		left := int16(0)
		if homeNode.Children[i].Orders != nil {
			left = *homeNode.Children[i].Orders
		}
		right := int16(0)
		if homeNode.Children[j].Orders != nil {
			right = *homeNode.Children[j].Orders
		}
		return left < right
	})
}

func findUiElementByCode(nodes []*model.UiElementsListRsp, code string) *model.UiElementsListRsp {
	for _, node := range nodes {
		if node.ElementCode == code {
			return node
		}
		if len(node.Children) > 0 {
			if child := findUiElementByCode(node.Children, code); child != nil {
				return child
			}
		}
	}
	return nil
}

func buildDashboardMenuRouteCode(dashboardID string) string {
	replacer := strings.NewReplacer("-", "_", " ", "_", "/", "_")
	return "home_dashboard_" + replacer.Replace(dashboardID)
}

func int16Ptr(value int16) *int16 {
	return &value
}
