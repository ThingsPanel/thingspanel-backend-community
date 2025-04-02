package service

import global "project/pkg/global"

type Casbin struct {
}

// 角色添加多个功能
func (*Casbin) AddFunctionToRole(role string, functions []string) bool {
	var rules [][]string
	for _, function := range functions {
		rule := []string{role, function, "allow"}
		rules = append(rules, rule)
	}
	isSuccess, _ := global.CasbinEnforcer.AddNamedPolicies("p", rules)
	return isSuccess
}

// 查询角色的功能
func (*Casbin) GetFunctionFromRole(role string) ([]string, bool) {
	policys := global.CasbinEnforcer.GetFilteredPolicy(0, role)
	var functions []string
	for _, policy := range policys {
		functions = append(functions, policy[1])
	}
	return functions, true
}

// 删除角色和功能
func (*Casbin) RemoveRoleAndFunction(role string) bool {
	isSuccess, _ := global.CasbinEnforcer.RemoveFilteredPolicy(0, role)
	return isSuccess

}

// 用户添加多个角色
func (*Casbin) AddRolesToUser(user string, roles []string) bool {
	var rules [][]string
	for _, role := range roles {
		rule := []string{user, role}
		rules = append(rules, rule)
	}
	isSuccess, _ := global.CasbinEnforcer.AddNamedGroupingPolicies("g", rules)
	return isSuccess
}

// 查询用户的角色
func (*Casbin) GetRoleFromUser(user string) ([]string, bool) {
	policys := global.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g", 0, user)
	var roles []string
	for _, policy := range policys {
		roles = append(roles, policy[1])
	}
	return roles, true
}

// 删除用户和角色
func (*Casbin) RemoveUserAndRole(user string) bool {
	isSuccess, _ := global.CasbinEnforcer.RemoveFilteredNamedGroupingPolicy("g", 0, user)
	return isSuccess
}

// 查询是否存在某个资源
func (*Casbin) GetUrl(url string) bool {
	stringList := global.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g2", 0, url)
	return len(stringList) != 0
}

// 查询用户角色中是否存在某个角色
func (*Casbin) HasRole(role string) bool {
	stringList := global.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g", 1, role)
	return len(stringList) != 0
}

// 校验
func (*Casbin) Verify(user string, url string) bool {
	isTrue, _ := global.CasbinEnforcer.Enforce(user, url, "allow")
	return isTrue
}
