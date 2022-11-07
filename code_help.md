# 编码帮助

- errors.New("")
- time.Now().Format("2006-01-02 15:04:05")
- var A map[string]interface{}  
  result := psql.Mydb.Raw("select * from t where a = ?", a).Scan(&A)