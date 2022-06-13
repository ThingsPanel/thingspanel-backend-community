package services

import (
	casbin "ThingsPanel-Go/initialize/casbin"
)

type CasbinService struct {
}

// 角色添加多个功能
func (*CasbinService) AddFunctionToRole(role string, functions []string) bool {
	rules := [][]string{}
	for _, function := range functions {
		rule := []string{role, function, "allow"}
		rules = append(rules, rule)
	}
	isSuccess, _ := casbin.CasbinEnforcer.AddNamedPolicies("p", rules)
	return isSuccess
}

// 查询角色的功能
func (*CasbinService) GetFunctionFromRole(role string) ([]string, bool) {
	policys := casbin.CasbinEnforcer.GetFilteredPolicy(0, role)
	var functions []string
	for _, policy := range policys {
		functions = append(functions, policy[1])
	}
	return functions, true
}

// 删除角色和功能
func (*CasbinService) RemoveRoleAndFunction(role string) bool {
	isSuccess, _ := casbin.CasbinEnforcer.RemoveFilteredPolicy(0, role)
	return isSuccess

}

// 用户添加多个角色
func (*CasbinService) AddRolesToUser(user string, roles []string) bool {
	rules := [][]string{}
	for _, role := range roles {
		rule := []string{user, role}
		rules = append(rules, rule)
	}
	isSuccess, _ := casbin.CasbinEnforcer.AddNamedGroupingPolicies("g", rules)
	return isSuccess
}

// 查询用户的角色
func (*CasbinService) GetRoleFromUser(user string) ([]string, bool) {
	policys := casbin.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g", 0, user)
	var roles []string
	for _, policy := range policys {
		roles = append(roles, policy[1])
	}
	return roles, true
}

// 删除用户和角色
func (*CasbinService) RemoveUserAndRole(user string) bool {
	isSuccess, _ := casbin.CasbinEnforcer.RemoveFilteredNamedGroupingPolicy("g", 0, user)
	return isSuccess
}
