package main

import (
	initialize "project/initialize"

	"gorm.io/gen"
)

func main() {
	g := gen.NewGenerator(gen.Config{
		OutPath:       "../../internal/query",
		Mode:          gen.WithoutContext | gen.WithDefaultQuery | gen.WithQueryInterface, // generate mode
		FieldNullable: true,
	})

	// gormdb, _ := gorm.Open(mysql.Open("root:@(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True&loc=Local"))
	initialize.ViperInit("../../configs/conf-dev.yml")
	initialize.LogInIt()
	gormdb, err := initialize.PgInit()
	if err != nil {
		panic(err)
	}
	if gormdb == nil {
		panic("gormdb is nil")
	}
	g.UseDB(gormdb) // reuse your gorm db

	// Generate basic type-safe DAO API for struct `model.User` following conventions

	// g.ApplyBasic(
	// 	// Generate struct `User` based on table `users`
	// 	g.GenerateModel("sys_dict_language"),

	// 	// Generate struct `Employee` based on table `users`
	// 	// g.GenerateModelAs("users", "Employee"),

	// 	// Generate struct `User` based on table `users` and generating options
	// 	// g.GenerateModel("users", gen.FieldIgnore("name"), gen.FieldType("id", "int64")),
	// )
	g.ApplyBasic(
		// Generate structs from all tables of current database
		//	g.GenerateAllTable()...,
		// 生成sys_ui_elements表的model和query
		g.GenerateModel("device_topic_mappings"),
	)
	// Generate the code
	g.Execute()
}
