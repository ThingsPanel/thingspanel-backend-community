# 编码帮助

- errors.New("")
- time.Now().Format("2006-01-02 15:04:05")
- var A map[string]interface{}  
  result := psql.Mydb.Raw("select * from t where a = ?", a).Scan(&A)
- 数值转换包
github.com/spf13/cast 
- 根据数据库表结构生成model
bee generate appcode -tables="tp_scenario_action" -driver=postgres -conn "postgres://postgres:postgresThingsPanel2022@127.0.0.1:5432/ThingsPanel?sslmode=disable" -level=1

- 多条件查询避免sql注入
var paramList []interface{}
if PaginationValidate.AutomationLogId != "" {
		sqlWhere += " and automation_log_id = ?"
		paramList = append(paramList, PaginationValidate.AutomationLogId)
	}
	var count int64
	psql.Mydb.Model(&models.TpAutomationLogDetail{}).Where(sqlWhere, paramList...).Count(&count)

