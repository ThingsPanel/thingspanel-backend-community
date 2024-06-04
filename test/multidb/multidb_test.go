package main

import (
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"testing"
)

// 定义数据库表结构
type User struct {
	gorm.Model
	Name string ``
	Age  int
}

func TestMultiDb(t *testing.T) {
	require := require.New(t)
	whichDb := "pg" // 选择数据库类型，pg或mysql

	var db *gorm.DB
	var err error
	switch whichDb {
	case "pg":
		db, err = getPgDB()
	case "mysql":
		db, err = getMysqlDB()
	}

	require.NoError(err)

	// 数据库建表
	db.AutoMigrate(&User{})
	t.Log("Database created successfully")

	user := User{Name: "Jinzhu", Age: 18}

	result := db.Create(&user) // pass pointer of data to Create

	require.NoError(result.Error)

	// user.Name
	// user.ID             // returns inserted data's primary key
	// result.RowsAffected // returns inserted records count

	t.Logf("User created successfully:\n Name: %v\n age: %v\n created_at: %v\n updated_at: %v\n deleted_at: %v\n", user.Name, user.Age, user.CreatedAt, user.UpdatedAt, user.DeletedAt)
}

func getPgDB() (*gorm.DB, error) {
	dsn := "host=localhost user=postgres password=ThingsPanel2023 dbname=postgres port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	return db, err
}

func getMysqlDB() (*gorm.DB, error) {
	dsn := "root:example@tcp(localhost:3306)/mydb?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	return db, err
}
