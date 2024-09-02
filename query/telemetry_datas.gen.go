// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.

package query

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"gorm.io/gen"
	"gorm.io/gen/field"

	"gorm.io/plugin/dbresolver"

	"project/internal/model"
)

func newTelemetryData(db *gorm.DB, opts ...gen.DOOption) telemetryData {
	_telemetryData := telemetryData{}

	_telemetryData.telemetryDataDo.UseDB(db, opts...)
	_telemetryData.telemetryDataDo.UseModel(&model.TelemetryData{})

	tableName := _telemetryData.telemetryDataDo.TableName()
	_telemetryData.ALL = field.NewAsterisk(tableName)
	_telemetryData.DeviceID = field.NewString(tableName, "device_id")
	_telemetryData.Key = field.NewString(tableName, "key")
	_telemetryData.T = field.NewInt64(tableName, "ts")
	_telemetryData.BoolV = field.NewBool(tableName, "bool_v")
	_telemetryData.NumberV = field.NewFloat64(tableName, "number_v")
	_telemetryData.StringV = field.NewString(tableName, "string_v")
	_telemetryData.TenantID = field.NewString(tableName, "tenant_id")

	_telemetryData.fillFieldMap()

	return _telemetryData
}

type telemetryData struct {
	telemetryDataDo

	ALL      field.Asterisk
	DeviceID field.String // 设备ID
	Key      field.String // 数据标识符
	T        field.Int64  // 上报时间
	BoolV    field.Bool
	NumberV  field.Float64
	StringV  field.String
	TenantID field.String

	fieldMap map[string]field.Expr
}

func (t telemetryData) Table(newTableName string) *telemetryData {
	t.telemetryDataDo.UseTable(newTableName)
	return t.updateTableName(newTableName)
}

func (t telemetryData) As(alias string) *telemetryData {
	t.telemetryDataDo.DO = *(t.telemetryDataDo.As(alias).(*gen.DO))
	return t.updateTableName(alias)
}

func (t *telemetryData) updateTableName(table string) *telemetryData {
	t.ALL = field.NewAsterisk(table)
	t.DeviceID = field.NewString(table, "device_id")
	t.Key = field.NewString(table, "key")
	t.T = field.NewInt64(table, "ts")
	t.BoolV = field.NewBool(table, "bool_v")
	t.NumberV = field.NewFloat64(table, "number_v")
	t.StringV = field.NewString(table, "string_v")
	t.TenantID = field.NewString(table, "tenant_id")

	t.fillFieldMap()

	return t
}

func (t *telemetryData) GetFieldByName(fieldName string) (field.OrderExpr, bool) {
	_f, ok := t.fieldMap[fieldName]
	if !ok || _f == nil {
		return nil, false
	}
	_oe, ok := _f.(field.OrderExpr)
	return _oe, ok
}

func (t *telemetryData) fillFieldMap() {
	t.fieldMap = make(map[string]field.Expr, 7)
	t.fieldMap["device_id"] = t.DeviceID
	t.fieldMap["key"] = t.Key
	t.fieldMap["ts"] = t.T
	t.fieldMap["bool_v"] = t.BoolV
	t.fieldMap["number_v"] = t.NumberV
	t.fieldMap["string_v"] = t.StringV
	t.fieldMap["tenant_id"] = t.TenantID
}

func (t telemetryData) clone(db *gorm.DB) telemetryData {
	t.telemetryDataDo.ReplaceConnPool(db.Statement.ConnPool)
	return t
}

func (t telemetryData) replaceDB(db *gorm.DB) telemetryData {
	t.telemetryDataDo.ReplaceDB(db)
	return t
}

type telemetryDataDo struct{ gen.DO }

type ITelemetryDataDo interface {
	gen.SubQuery
	Debug() ITelemetryDataDo
	WithContext(ctx context.Context) ITelemetryDataDo
	WithResult(fc func(tx gen.Dao)) gen.ResultInfo
	ReplaceDB(db *gorm.DB)
	ReadDB() ITelemetryDataDo
	WriteDB() ITelemetryDataDo
	As(alias string) gen.Dao
	Session(config *gorm.Session) ITelemetryDataDo
	Columns(cols ...field.Expr) gen.Columns
	Clauses(conds ...clause.Expression) ITelemetryDataDo
	Not(conds ...gen.Condition) ITelemetryDataDo
	Or(conds ...gen.Condition) ITelemetryDataDo
	Select(conds ...field.Expr) ITelemetryDataDo
	Where(conds ...gen.Condition) ITelemetryDataDo
	Order(conds ...field.Expr) ITelemetryDataDo
	Distinct(cols ...field.Expr) ITelemetryDataDo
	Omit(cols ...field.Expr) ITelemetryDataDo
	Join(table schema.Tabler, on ...field.Expr) ITelemetryDataDo
	LeftJoin(table schema.Tabler, on ...field.Expr) ITelemetryDataDo
	RightJoin(table schema.Tabler, on ...field.Expr) ITelemetryDataDo
	Group(cols ...field.Expr) ITelemetryDataDo
	Having(conds ...gen.Condition) ITelemetryDataDo
	Limit(limit int) ITelemetryDataDo
	Offset(offset int) ITelemetryDataDo
	Count() (count int64, err error)
	Scopes(funcs ...func(gen.Dao) gen.Dao) ITelemetryDataDo
	Unscoped() ITelemetryDataDo
	Create(values ...*model.TelemetryData) error
	CreateInBatches(values []*model.TelemetryData, batchSize int) error
	Save(values ...*model.TelemetryData) error
	First() (*model.TelemetryData, error)
	Take() (*model.TelemetryData, error)
	Last() (*model.TelemetryData, error)
	Find() ([]*model.TelemetryData, error)
	FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.TelemetryData, err error)
	FindInBatches(result *[]*model.TelemetryData, batchSize int, fc func(tx gen.Dao, batch int) error) error
	Pluck(column field.Expr, dest interface{}) error
	Delete(...*model.TelemetryData) (info gen.ResultInfo, err error)
	Update(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	Updates(value interface{}) (info gen.ResultInfo, err error)
	UpdateColumn(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateColumnSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	UpdateColumns(value interface{}) (info gen.ResultInfo, err error)
	UpdateFrom(q gen.SubQuery) gen.Dao
	Attrs(attrs ...field.AssignExpr) ITelemetryDataDo
	Assign(attrs ...field.AssignExpr) ITelemetryDataDo
	Joins(fields ...field.RelationField) ITelemetryDataDo
	Preload(fields ...field.RelationField) ITelemetryDataDo
	FirstOrInit() (*model.TelemetryData, error)
	FirstOrCreate() (*model.TelemetryData, error)
	FindByPage(offset int, limit int) (result []*model.TelemetryData, count int64, err error)
	ScanByPage(result interface{}, offset int, limit int) (count int64, err error)
	Scan(result interface{}) (err error)
	Returning(value interface{}, columns ...string) ITelemetryDataDo
	UnderlyingDB() *gorm.DB
	schema.Tabler
}

func (t telemetryDataDo) Debug() ITelemetryDataDo {
	return t.withDO(t.DO.Debug())
}

func (t telemetryDataDo) WithContext(ctx context.Context) ITelemetryDataDo {
	return t.withDO(t.DO.WithContext(ctx))
}

func (t telemetryDataDo) ReadDB() ITelemetryDataDo {
	return t.Clauses(dbresolver.Read)
}

func (t telemetryDataDo) WriteDB() ITelemetryDataDo {
	return t.Clauses(dbresolver.Write)
}

func (t telemetryDataDo) Session(config *gorm.Session) ITelemetryDataDo {
	return t.withDO(t.DO.Session(config))
}

func (t telemetryDataDo) Clauses(conds ...clause.Expression) ITelemetryDataDo {
	return t.withDO(t.DO.Clauses(conds...))
}

func (t telemetryDataDo) Returning(value interface{}, columns ...string) ITelemetryDataDo {
	return t.withDO(t.DO.Returning(value, columns...))
}

func (t telemetryDataDo) Not(conds ...gen.Condition) ITelemetryDataDo {
	return t.withDO(t.DO.Not(conds...))
}

func (t telemetryDataDo) Or(conds ...gen.Condition) ITelemetryDataDo {
	return t.withDO(t.DO.Or(conds...))
}

func (t telemetryDataDo) Select(conds ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.Select(conds...))
}

func (t telemetryDataDo) Where(conds ...gen.Condition) ITelemetryDataDo {
	return t.withDO(t.DO.Where(conds...))
}

func (t telemetryDataDo) Order(conds ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.Order(conds...))
}

func (t telemetryDataDo) Distinct(cols ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.Distinct(cols...))
}

func (t telemetryDataDo) Omit(cols ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.Omit(cols...))
}

func (t telemetryDataDo) Join(table schema.Tabler, on ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.Join(table, on...))
}

func (t telemetryDataDo) LeftJoin(table schema.Tabler, on ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.LeftJoin(table, on...))
}

func (t telemetryDataDo) RightJoin(table schema.Tabler, on ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.RightJoin(table, on...))
}

func (t telemetryDataDo) Group(cols ...field.Expr) ITelemetryDataDo {
	return t.withDO(t.DO.Group(cols...))
}

func (t telemetryDataDo) Having(conds ...gen.Condition) ITelemetryDataDo {
	return t.withDO(t.DO.Having(conds...))
}

func (t telemetryDataDo) Limit(limit int) ITelemetryDataDo {
	return t.withDO(t.DO.Limit(limit))
}

func (t telemetryDataDo) Offset(offset int) ITelemetryDataDo {
	return t.withDO(t.DO.Offset(offset))
}

func (t telemetryDataDo) Scopes(funcs ...func(gen.Dao) gen.Dao) ITelemetryDataDo {
	return t.withDO(t.DO.Scopes(funcs...))
}

func (t telemetryDataDo) Unscoped() ITelemetryDataDo {
	return t.withDO(t.DO.Unscoped())
}

func (t telemetryDataDo) Create(values ...*model.TelemetryData) error {
	if len(values) == 0 {
		return nil
	}
	return t.DO.Create(values)
}

func (t telemetryDataDo) CreateInBatches(values []*model.TelemetryData, batchSize int) error {
	return t.DO.CreateInBatches(values, batchSize)
}

// Save : !!! underlying implementation is different with GORM
// The method is equivalent to executing the statement: db.Clauses(clause.OnConflict{UpdateAll: true}).Create(values)
func (t telemetryDataDo) Save(values ...*model.TelemetryData) error {
	if len(values) == 0 {
		return nil
	}
	return t.DO.Save(values)
}

func (t telemetryDataDo) First() (*model.TelemetryData, error) {
	if result, err := t.DO.First(); err != nil {
		return nil, err
	} else {
		return result.(*model.TelemetryData), nil
	}
}

func (t telemetryDataDo) Take() (*model.TelemetryData, error) {
	if result, err := t.DO.Take(); err != nil {
		return nil, err
	} else {
		return result.(*model.TelemetryData), nil
	}
}

func (t telemetryDataDo) Last() (*model.TelemetryData, error) {
	if result, err := t.DO.Last(); err != nil {
		return nil, err
	} else {
		return result.(*model.TelemetryData), nil
	}
}

func (t telemetryDataDo) Find() ([]*model.TelemetryData, error) {
	result, err := t.DO.Find()
	return result.([]*model.TelemetryData), err
}

func (t telemetryDataDo) FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.TelemetryData, err error) {
	buf := make([]*model.TelemetryData, 0, batchSize)
	err = t.DO.FindInBatches(&buf, batchSize, func(tx gen.Dao, batch int) error {
		defer func() { results = append(results, buf...) }()
		return fc(tx, batch)
	})
	return results, err
}

func (t telemetryDataDo) FindInBatches(result *[]*model.TelemetryData, batchSize int, fc func(tx gen.Dao, batch int) error) error {
	return t.DO.FindInBatches(result, batchSize, fc)
}

func (t telemetryDataDo) Attrs(attrs ...field.AssignExpr) ITelemetryDataDo {
	return t.withDO(t.DO.Attrs(attrs...))
}

func (t telemetryDataDo) Assign(attrs ...field.AssignExpr) ITelemetryDataDo {
	return t.withDO(t.DO.Assign(attrs...))
}

func (t telemetryDataDo) Joins(fields ...field.RelationField) ITelemetryDataDo {
	for _, _f := range fields {
		t = *t.withDO(t.DO.Joins(_f))
	}
	return &t
}

func (t telemetryDataDo) Preload(fields ...field.RelationField) ITelemetryDataDo {
	for _, _f := range fields {
		t = *t.withDO(t.DO.Preload(_f))
	}
	return &t
}

func (t telemetryDataDo) FirstOrInit() (*model.TelemetryData, error) {
	if result, err := t.DO.FirstOrInit(); err != nil {
		return nil, err
	} else {
		return result.(*model.TelemetryData), nil
	}
}

func (t telemetryDataDo) FirstOrCreate() (*model.TelemetryData, error) {
	if result, err := t.DO.FirstOrCreate(); err != nil {
		return nil, err
	} else {
		return result.(*model.TelemetryData), nil
	}
}

func (t telemetryDataDo) FindByPage(offset int, limit int) (result []*model.TelemetryData, count int64, err error) {
	result, err = t.Offset(offset).Limit(limit).Find()
	if err != nil {
		return
	}

	if size := len(result); 0 < limit && 0 < size && size < limit {
		count = int64(size + offset)
		return
	}

	count, err = t.Offset(-1).Limit(-1).Count()
	return
}

func (t telemetryDataDo) ScanByPage(result interface{}, offset int, limit int) (count int64, err error) {
	count, err = t.Count()
	if err != nil {
		return
	}

	err = t.Offset(offset).Limit(limit).Scan(result)
	return
}

func (t telemetryDataDo) Scan(result interface{}) (err error) {
	return t.DO.Scan(result)
}

func (t telemetryDataDo) Delete(models ...*model.TelemetryData) (result gen.ResultInfo, err error) {
	return t.DO.Delete(models)
}

func (t *telemetryDataDo) withDO(do gen.Dao) *telemetryDataDo {
	t.DO = *do.(*gen.DO)
	return t
}
