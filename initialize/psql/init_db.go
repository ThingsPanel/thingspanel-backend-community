package psql

import (
	"database/sql"
	"fmt"
	"io/ioutil"

	_ "github.com/lib/pq"
)

func init_db(user string, password string, dbname string, host string, port int) {
	// 数据库连接信息
	connectionString := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%d sslmode=disable", user, password, dbname, host, port)

	// 连接数据库
	db, err := sql.Open("postgres", connectionString)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// 判断表是否存在
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'ts_kv')").Scan(&exists)
	if err != nil {
		panic(err)
	}

	if !exists {
		fmt.Println("读取sql脚本...")
		// 读取 SQL 脚本文件
		sqlFile, err := ioutil.ReadFile("TP.sql")
		if err != nil {
			panic(err)
		}
		fmt.Println("执行sql脚本...")
		// 执行 SQL 脚本
		_, err = db.Exec(string(sqlFile))
		if err != nil {
			panic(err)
		}
		fmt.Println("SQL script executed successfully!")
	} else {
		fmt.Println("无需执行sql脚本")
	}
}
