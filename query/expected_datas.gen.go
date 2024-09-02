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

func newExpectedData(db *gorm.DB, opts ...gen.DOOption) expectedData {
	_expectedData := expectedData{}

	_expectedData.expectedDataDo.UseDB(db, opts...)
	_expectedData.expectedDataDo.UseModel(&model.ExpectedData{})

	tableName := _expectedData.expectedDataDo.TableName()
	_expectedData.ALL = field.NewAsterisk(tableName)
	_expectedData.ID = field.NewString(tableName, "id")
	_expectedData.DeviceID = field.NewString(tableName, "device_id")
	_expectedData.SendType = field.NewString(tableName, "send_type")
	_expectedData.Payload = field.NewString(tableName, "payload")
	_expectedData.CreatedAt = field.NewTime(tableName, "created_at")
	_expectedData.SendTime = field.NewTime(tableName, "send_time")
	_expectedData.Status = field.NewString(tableName, "status")
	_expectedData.Message = field.NewString(tableName, "message")
	_expectedData.ExpiryTime = field.NewTime(tableName, "expiry_time")
	_expectedData.Label = field.NewString(tableName, "label")
	_expectedData.TenantID = field.NewString(tableName, "tenant_id")

	_expectedData.fillFieldMap()

	return _expectedData
}

type expectedData struct {
	expectedDataDo

	ALL        field.Asterisk
	ID         field.String // 指令唯一标识符(UUID)
	DeviceID   field.String // 目标设备ID
	SendType   field.String // 指令类型(e.g., telemetry, attribute, command)
	Payload    field.String // 指令内容(具体指令参数)
	CreatedAt  field.Time   // 指令生成时间
	SendTime   field.Time   // 指令实际发送时间(如果已发送)
	Status     field.String // 指令状态(pending, sent, expired)，默认待发送
	Message    field.String // 状态附加信息(如发送失败的原因)
	ExpiryTime field.Time   // 指令过期时间(可选)
	Label      field.String // 指令标签(可选)
	TenantID   field.String // 租户ID（用于多租户系统）

	fieldMap map[string]field.Expr
}

func (e expectedData) Table(newTableName string) *expectedData {
	e.expectedDataDo.UseTable(newTableName)
	return e.updateTableName(newTableName)
}

func (e expectedData) As(alias string) *expectedData {
	e.expectedDataDo.DO = *(e.expectedDataDo.As(alias).(*gen.DO))
	return e.updateTableName(alias)
}

func (e *expectedData) updateTableName(table string) *expectedData {
	e.ALL = field.NewAsterisk(table)
	e.ID = field.NewString(table, "id")
	e.DeviceID = field.NewString(table, "device_id")
	e.SendType = field.NewString(table, "send_type")
	e.Payload = field.NewString(table, "payload")
	e.CreatedAt = field.NewTime(table, "created_at")
	e.SendTime = field.NewTime(table, "send_time")
	e.Status = field.NewString(table, "status")
	e.Message = field.NewString(table, "message")
	e.ExpiryTime = field.NewTime(table, "expiry_time")
	e.Label = field.NewString(table, "label")
	e.TenantID = field.NewString(table, "tenant_id")

	e.fillFieldMap()

	return e
}

func (e *expectedData) GetFieldByName(fieldName string) (field.OrderExpr, bool) {
	_f, ok := e.fieldMap[fieldName]
	if !ok || _f == nil {
		return nil, false
	}
	_oe, ok := _f.(field.OrderExpr)
	return _oe, ok
}

func (e *expectedData) fillFieldMap() {
	e.fieldMap = make(map[string]field.Expr, 11)
	e.fieldMap["id"] = e.ID
	e.fieldMap["device_id"] = e.DeviceID
	e.fieldMap["send_type"] = e.SendType
	e.fieldMap["payload"] = e.Payload
	e.fieldMap["created_at"] = e.CreatedAt
	e.fieldMap["send_time"] = e.SendTime
	e.fieldMap["status"] = e.Status
	e.fieldMap["message"] = e.Message
	e.fieldMap["expiry_time"] = e.ExpiryTime
	e.fieldMap["label"] = e.Label
	e.fieldMap["tenant_id"] = e.TenantID
}

func (e expectedData) clone(db *gorm.DB) expectedData {
	e.expectedDataDo.ReplaceConnPool(db.Statement.ConnPool)
	return e
}

func (e expectedData) replaceDB(db *gorm.DB) expectedData {
	e.expectedDataDo.ReplaceDB(db)
	return e
}

type expectedDataDo struct{ gen.DO }

type IExpectedDataDo interface {
	gen.SubQuery
	Debug() IExpectedDataDo
	WithContext(ctx context.Context) IExpectedDataDo
	WithResult(fc func(tx gen.Dao)) gen.ResultInfo
	ReplaceDB(db *gorm.DB)
	ReadDB() IExpectedDataDo
	WriteDB() IExpectedDataDo
	As(alias string) gen.Dao
	Session(config *gorm.Session) IExpectedDataDo
	Columns(cols ...field.Expr) gen.Columns
	Clauses(conds ...clause.Expression) IExpectedDataDo
	Not(conds ...gen.Condition) IExpectedDataDo
	Or(conds ...gen.Condition) IExpectedDataDo
	Select(conds ...field.Expr) IExpectedDataDo
	Where(conds ...gen.Condition) IExpectedDataDo
	Order(conds ...field.Expr) IExpectedDataDo
	Distinct(cols ...field.Expr) IExpectedDataDo
	Omit(cols ...field.Expr) IExpectedDataDo
	Join(table schema.Tabler, on ...field.Expr) IExpectedDataDo
	LeftJoin(table schema.Tabler, on ...field.Expr) IExpectedDataDo
	RightJoin(table schema.Tabler, on ...field.Expr) IExpectedDataDo
	Group(cols ...field.Expr) IExpectedDataDo
	Having(conds ...gen.Condition) IExpectedDataDo
	Limit(limit int) IExpectedDataDo
	Offset(offset int) IExpectedDataDo
	Count() (count int64, err error)
	Scopes(funcs ...func(gen.Dao) gen.Dao) IExpectedDataDo
	Unscoped() IExpectedDataDo
	Create(values ...*model.ExpectedData) error
	CreateInBatches(values []*model.ExpectedData, batchSize int) error
	Save(values ...*model.ExpectedData) error
	First() (*model.ExpectedData, error)
	Take() (*model.ExpectedData, error)
	Last() (*model.ExpectedData, error)
	Find() ([]*model.ExpectedData, error)
	FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.ExpectedData, err error)
	FindInBatches(result *[]*model.ExpectedData, batchSize int, fc func(tx gen.Dao, batch int) error) error
	Pluck(column field.Expr, dest interface{}) error
	Delete(...*model.ExpectedData) (info gen.ResultInfo, err error)
	Update(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	Updates(value interface{}) (info gen.ResultInfo, err error)
	UpdateColumn(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateColumnSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	UpdateColumns(value interface{}) (info gen.ResultInfo, err error)
	UpdateFrom(q gen.SubQuery) gen.Dao
	Attrs(attrs ...field.AssignExpr) IExpectedDataDo
	Assign(attrs ...field.AssignExpr) IExpectedDataDo
	Joins(fields ...field.RelationField) IExpectedDataDo
	Preload(fields ...field.RelationField) IExpectedDataDo
	FirstOrInit() (*model.ExpectedData, error)
	FirstOrCreate() (*model.ExpectedData, error)
	FindByPage(offset int, limit int) (result []*model.ExpectedData, count int64, err error)
	ScanByPage(result interface{}, offset int, limit int) (count int64, err error)
	Scan(result interface{}) (err error)
	Returning(value interface{}, columns ...string) IExpectedDataDo
	UnderlyingDB() *gorm.DB
	schema.Tabler
}

func (e expectedDataDo) Debug() IExpectedDataDo {
	return e.withDO(e.DO.Debug())
}

func (e expectedDataDo) WithContext(ctx context.Context) IExpectedDataDo {
	return e.withDO(e.DO.WithContext(ctx))
}

func (e expectedDataDo) ReadDB() IExpectedDataDo {
	return e.Clauses(dbresolver.Read)
}

func (e expectedDataDo) WriteDB() IExpectedDataDo {
	return e.Clauses(dbresolver.Write)
}

func (e expectedDataDo) Session(config *gorm.Session) IExpectedDataDo {
	return e.withDO(e.DO.Session(config))
}

func (e expectedDataDo) Clauses(conds ...clause.Expression) IExpectedDataDo {
	return e.withDO(e.DO.Clauses(conds...))
}

func (e expectedDataDo) Returning(value interface{}, columns ...string) IExpectedDataDo {
	return e.withDO(e.DO.Returning(value, columns...))
}

func (e expectedDataDo) Not(conds ...gen.Condition) IExpectedDataDo {
	return e.withDO(e.DO.Not(conds...))
}

func (e expectedDataDo) Or(conds ...gen.Condition) IExpectedDataDo {
	return e.withDO(e.DO.Or(conds...))
}

func (e expectedDataDo) Select(conds ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.Select(conds...))
}

func (e expectedDataDo) Where(conds ...gen.Condition) IExpectedDataDo {
	return e.withDO(e.DO.Where(conds...))
}

func (e expectedDataDo) Order(conds ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.Order(conds...))
}

func (e expectedDataDo) Distinct(cols ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.Distinct(cols...))
}

func (e expectedDataDo) Omit(cols ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.Omit(cols...))
}

func (e expectedDataDo) Join(table schema.Tabler, on ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.Join(table, on...))
}

func (e expectedDataDo) LeftJoin(table schema.Tabler, on ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.LeftJoin(table, on...))
}

func (e expectedDataDo) RightJoin(table schema.Tabler, on ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.RightJoin(table, on...))
}

func (e expectedDataDo) Group(cols ...field.Expr) IExpectedDataDo {
	return e.withDO(e.DO.Group(cols...))
}

func (e expectedDataDo) Having(conds ...gen.Condition) IExpectedDataDo {
	return e.withDO(e.DO.Having(conds...))
}

func (e expectedDataDo) Limit(limit int) IExpectedDataDo {
	return e.withDO(e.DO.Limit(limit))
}

func (e expectedDataDo) Offset(offset int) IExpectedDataDo {
	return e.withDO(e.DO.Offset(offset))
}

func (e expectedDataDo) Scopes(funcs ...func(gen.Dao) gen.Dao) IExpectedDataDo {
	return e.withDO(e.DO.Scopes(funcs...))
}

func (e expectedDataDo) Unscoped() IExpectedDataDo {
	return e.withDO(e.DO.Unscoped())
}

func (e expectedDataDo) Create(values ...*model.ExpectedData) error {
	if len(values) == 0 {
		return nil
	}
	return e.DO.Create(values)
}

func (e expectedDataDo) CreateInBatches(values []*model.ExpectedData, batchSize int) error {
	return e.DO.CreateInBatches(values, batchSize)
}

// Save : !!! underlying implementation is different with GORM
// The method is equivalent to executing the statement: db.Clauses(clause.OnConflict{UpdateAll: true}).Create(values)
func (e expectedDataDo) Save(values ...*model.ExpectedData) error {
	if len(values) == 0 {
		return nil
	}
	return e.DO.Save(values)
}

func (e expectedDataDo) First() (*model.ExpectedData, error) {
	if result, err := e.DO.First(); err != nil {
		return nil, err
	} else {
		return result.(*model.ExpectedData), nil
	}
}

func (e expectedDataDo) Take() (*model.ExpectedData, error) {
	if result, err := e.DO.Take(); err != nil {
		return nil, err
	} else {
		return result.(*model.ExpectedData), nil
	}
}

func (e expectedDataDo) Last() (*model.ExpectedData, error) {
	if result, err := e.DO.Last(); err != nil {
		return nil, err
	} else {
		return result.(*model.ExpectedData), nil
	}
}

func (e expectedDataDo) Find() ([]*model.ExpectedData, error) {
	result, err := e.DO.Find()
	return result.([]*model.ExpectedData), err
}

func (e expectedDataDo) FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.ExpectedData, err error) {
	buf := make([]*model.ExpectedData, 0, batchSize)
	err = e.DO.FindInBatches(&buf, batchSize, func(tx gen.Dao, batch int) error {
		defer func() { results = append(results, buf...) }()
		return fc(tx, batch)
	})
	return results, err
}

func (e expectedDataDo) FindInBatches(result *[]*model.ExpectedData, batchSize int, fc func(tx gen.Dao, batch int) error) error {
	return e.DO.FindInBatches(result, batchSize, fc)
}

func (e expectedDataDo) Attrs(attrs ...field.AssignExpr) IExpectedDataDo {
	return e.withDO(e.DO.Attrs(attrs...))
}

func (e expectedDataDo) Assign(attrs ...field.AssignExpr) IExpectedDataDo {
	return e.withDO(e.DO.Assign(attrs...))
}

func (e expectedDataDo) Joins(fields ...field.RelationField) IExpectedDataDo {
	for _, _f := range fields {
		e = *e.withDO(e.DO.Joins(_f))
	}
	return &e
}

func (e expectedDataDo) Preload(fields ...field.RelationField) IExpectedDataDo {
	for _, _f := range fields {
		e = *e.withDO(e.DO.Preload(_f))
	}
	return &e
}

func (e expectedDataDo) FirstOrInit() (*model.ExpectedData, error) {
	if result, err := e.DO.FirstOrInit(); err != nil {
		return nil, err
	} else {
		return result.(*model.ExpectedData), nil
	}
}

func (e expectedDataDo) FirstOrCreate() (*model.ExpectedData, error) {
	if result, err := e.DO.FirstOrCreate(); err != nil {
		return nil, err
	} else {
		return result.(*model.ExpectedData), nil
	}
}

func (e expectedDataDo) FindByPage(offset int, limit int) (result []*model.ExpectedData, count int64, err error) {
	result, err = e.Offset(offset).Limit(limit).Find()
	if err != nil {
		return
	}

	if size := len(result); 0 < limit && 0 < size && size < limit {
		count = int64(size + offset)
		return
	}

	count, err = e.Offset(-1).Limit(-1).Count()
	return
}

func (e expectedDataDo) ScanByPage(result interface{}, offset int, limit int) (count int64, err error) {
	count, err = e.Count()
	if err != nil {
		return
	}

	err = e.Offset(offset).Limit(limit).Scan(result)
	return
}

func (e expectedDataDo) Scan(result interface{}) (err error) {
	return e.DO.Scan(result)
}

func (e expectedDataDo) Delete(models ...*model.ExpectedData) (result gen.ResultInfo, err error) {
	return e.DO.Delete(models)
}

func (e *expectedDataDo) withDO(do gen.Dao) *expectedDataDo {
	e.DO = *do.(*gen.DO)
	return e
}
