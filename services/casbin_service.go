package services

import (
	casbin "ThingsPanel-Go/initialize/casbin"
)

type CasbinService struct {
}

// 获取所有功能数组-[* 用户管理 设备管理]
func (*CasbinService) GetAllSubjects() []string {
	subjects := casbin.CasbinEnforcer.GetAllNamedSubjects("p")
	return subjects
}

// 获取所有角色数组-[* 用户管理 设备管理]
func (*CasbinService) GetAllRoleGrouping() []string {
	roles := casbin.CasbinEnforcer.GetAllNamedSubjects("g")
	return roles
}

// 获取用户的角色-[* 用户管理 设备管理]
func (*CasbinService) GetUserRoleGrouping() []string {
	roles := casbin.CasbinEnforcer.GetAllNamedSubjects("g2")
	return roles
}

// 给角色添加功能[["系统管理员", "功能A"],["系统管理员", "功能B"]]
func (*CasbinService) AddRoleFunction(role string, functions []string) bool {
	rules := [][]string{}
	for _, value := range functions {
		rule := []string{role, value}
		rules = append(rules, rule)
	}
	areRulesAdded, _ := casbin.CasbinEnforcer.AddNamedGroupingPolicies("g", rules)
	return areRulesAdded
}

// 给用户分角色[["用户名", "角色A"],["用户名", "角色B"]]-系统暂不使用
func (*CasbinService) AddGroupingRole(user string, roles []string) bool {
	groups := [][]string{}
	for _, value := range roles {
		group := []string{user, value}
		groups = append(groups, group)
	}
	areRulesAdded, _ := casbin.CasbinEnforcer.AddNamedGroupingPolicies("g2", groups)
	return areRulesAdded
}
