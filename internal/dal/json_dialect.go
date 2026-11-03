package dal

import (
	"gorm.io/datatypes"
	"gorm.io/gen"

	global "project/pkg/global"
)

// jsonContainsString is the condition "the JSON column holds this string": an element of an array of strings, or
// a top-level object key. PostgreSQL answers it with the jsonb `?` operator (what datatypes.JSONQuery.HasKey
// renders there). MySQL's JSONQuery.HasKey looks a key up in an object and is NULL for an array, so a MySQL
// platform would see no menus and no alarm devices; JSON_CONTAINS(col, JSON_ARRAY(value)) (datatypes.JSONArrayQuery) matches the array form, which is the
// only form the callers store. gen.Cond accepts only the datatypes expressions, so a raw clause.Expr cannot be used.
func jsonContainsString(column, value string) []gen.Condition {
	if global.DB != nil && global.DB.Dialector.Name() == "mysql" {
		return gen.Cond(datatypes.JSONArrayQuery(column).Contains(value))
	}
	return gen.Cond(datatypes.JSONQuery(column).HasKey(value))
}
