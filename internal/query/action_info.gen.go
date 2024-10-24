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

func newActionInfo(db *gorm.DB, opts ...gen.DOOption) actionInfo {
	_actionInfo := actionInfo{}

	_actionInfo.actionInfoDo.UseDB(db, opts...)
	_actionInfo.actionInfoDo.UseModel(&model.ActionInfo{})

	tableName := _actionInfo.actionInfoDo.TableName()
	_actionInfo.ALL = field.NewAsterisk(tableName)
	_actionInfo.ID = field.NewString(tableName, "id")
	_actionInfo.SceneAutomationID = field.NewString(tableName, "scene_automation_id")
	_actionInfo.ActionTarget = field.NewString(tableName, "action_target")
	_actionInfo.ActionType = field.NewString(tableName, "action_type")
	_actionInfo.ActionParamType = field.NewString(tableName, "action_param_type")
	_actionInfo.ActionParam = field.NewString(tableName, "action_param")
	_actionInfo.ActionValue = field.NewString(tableName, "action_value")
	_actionInfo.Remark = field.NewString(tableName, "remark")

	_actionInfo.fillFieldMap()

	return _actionInfo
}

type actionInfo struct {
	actionInfoDo

	ALL               field.Asterisk
	ID                field.String
	SceneAutomationID field.String // 场景联动ID（外键-关联删除）
	ActionTarget      field.String // 动作目标id设备id、场景id、告警id；如果条件是单类设备，这里为空
	ActionType        field.String // 动作类型10: 单个设备11: 单类设备20: 激活场景30: 触发告警40: 服务
	ActionParamType   field.String // 遥测TEL属性ATTR命令CMD
	ActionParam       field.String // 动作参数动作类型为10,11是有效 标识符
	ActionValue       field.String // 目标值
	Remark            field.String

	fieldMap map[string]field.Expr
}

func (a actionInfo) Table(newTableName string) *actionInfo {
	a.actionInfoDo.UseTable(newTableName)
	return a.updateTableName(newTableName)
}

func (a actionInfo) As(alias string) *actionInfo {
	a.actionInfoDo.DO = *(a.actionInfoDo.As(alias).(*gen.DO))
	return a.updateTableName(alias)
}

func (a *actionInfo) updateTableName(table string) *actionInfo {
	a.ALL = field.NewAsterisk(table)
	a.ID = field.NewString(table, "id")
	a.SceneAutomationID = field.NewString(table, "scene_automation_id")
	a.ActionTarget = field.NewString(table, "action_target")
	a.ActionType = field.NewString(table, "action_type")
	a.ActionParamType = field.NewString(table, "action_param_type")
	a.ActionParam = field.NewString(table, "action_param")
	a.ActionValue = field.NewString(table, "action_value")
	a.Remark = field.NewString(table, "remark")

	a.fillFieldMap()

	return a
}

func (a *actionInfo) GetFieldByName(fieldName string) (field.OrderExpr, bool) {
	_f, ok := a.fieldMap[fieldName]
	if !ok || _f == nil {
		return nil, false
	}
	_oe, ok := _f.(field.OrderExpr)
	return _oe, ok
}

func (a *actionInfo) fillFieldMap() {
	a.fieldMap = make(map[string]field.Expr, 8)
	a.fieldMap["id"] = a.ID
	a.fieldMap["scene_automation_id"] = a.SceneAutomationID
	a.fieldMap["action_target"] = a.ActionTarget
	a.fieldMap["action_type"] = a.ActionType
	a.fieldMap["action_param_type"] = a.ActionParamType
	a.fieldMap["action_param"] = a.ActionParam
	a.fieldMap["action_value"] = a.ActionValue
	a.fieldMap["remark"] = a.Remark
}

func (a actionInfo) clone(db *gorm.DB) actionInfo {
	a.actionInfoDo.ReplaceConnPool(db.Statement.ConnPool)
	return a
}

func (a actionInfo) replaceDB(db *gorm.DB) actionInfo {
	a.actionInfoDo.ReplaceDB(db)
	return a
}

type actionInfoDo struct{ gen.DO }

type IActionInfoDo interface {
	gen.SubQuery
	Debug() IActionInfoDo
	WithContext(ctx context.Context) IActionInfoDo
	WithResult(fc func(tx gen.Dao)) gen.ResultInfo
	ReplaceDB(db *gorm.DB)
	ReadDB() IActionInfoDo
	WriteDB() IActionInfoDo
	As(alias string) gen.Dao
	Session(config *gorm.Session) IActionInfoDo
	Columns(cols ...field.Expr) gen.Columns
	Clauses(conds ...clause.Expression) IActionInfoDo
	Not(conds ...gen.Condition) IActionInfoDo
	Or(conds ...gen.Condition) IActionInfoDo
	Select(conds ...field.Expr) IActionInfoDo
	Where(conds ...gen.Condition) IActionInfoDo
	Order(conds ...field.Expr) IActionInfoDo
	Distinct(cols ...field.Expr) IActionInfoDo
	Omit(cols ...field.Expr) IActionInfoDo
	Join(table schema.Tabler, on ...field.Expr) IActionInfoDo
	LeftJoin(table schema.Tabler, on ...field.Expr) IActionInfoDo
	RightJoin(table schema.Tabler, on ...field.Expr) IActionInfoDo
	Group(cols ...field.Expr) IActionInfoDo
	Having(conds ...gen.Condition) IActionInfoDo
	Limit(limit int) IActionInfoDo
	Offset(offset int) IActionInfoDo
	Count() (count int64, err error)
	Scopes(funcs ...func(gen.Dao) gen.Dao) IActionInfoDo
	Unscoped() IActionInfoDo
	Create(values ...*model.ActionInfo) error
	CreateInBatches(values []*model.ActionInfo, batchSize int) error
	Save(values ...*model.ActionInfo) error
	First() (*model.ActionInfo, error)
	Take() (*model.ActionInfo, error)
	Last() (*model.ActionInfo, error)
	Find() ([]*model.ActionInfo, error)
	FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.ActionInfo, err error)
	FindInBatches(result *[]*model.ActionInfo, batchSize int, fc func(tx gen.Dao, batch int) error) error
	Pluck(column field.Expr, dest interface{}) error
	Delete(...*model.ActionInfo) (info gen.ResultInfo, err error)
	Update(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	Updates(value interface{}) (info gen.ResultInfo, err error)
	UpdateColumn(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateColumnSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	UpdateColumns(value interface{}) (info gen.ResultInfo, err error)
	UpdateFrom(q gen.SubQuery) gen.Dao
	Attrs(attrs ...field.AssignExpr) IActionInfoDo
	Assign(attrs ...field.AssignExpr) IActionInfoDo
	Joins(fields ...field.RelationField) IActionInfoDo
	Preload(fields ...field.RelationField) IActionInfoDo
	FirstOrInit() (*model.ActionInfo, error)
	FirstOrCreate() (*model.ActionInfo, error)
	FindByPage(offset int, limit int) (result []*model.ActionInfo, count int64, err error)
	ScanByPage(result interface{}, offset int, limit int) (count int64, err error)
	Scan(result interface{}) (err error)
	Returning(value interface{}, columns ...string) IActionInfoDo
	UnderlyingDB() *gorm.DB
	schema.Tabler
}

func (a actionInfoDo) Debug() IActionInfoDo {
	return a.withDO(a.DO.Debug())
}

func (a actionInfoDo) WithContext(ctx context.Context) IActionInfoDo {
	return a.withDO(a.DO.WithContext(ctx))
}

func (a actionInfoDo) ReadDB() IActionInfoDo {
	return a.Clauses(dbresolver.Read)
}

func (a actionInfoDo) WriteDB() IActionInfoDo {
	return a.Clauses(dbresolver.Write)
}

func (a actionInfoDo) Session(config *gorm.Session) IActionInfoDo {
	return a.withDO(a.DO.Session(config))
}

func (a actionInfoDo) Clauses(conds ...clause.Expression) IActionInfoDo {
	return a.withDO(a.DO.Clauses(conds...))
}

func (a actionInfoDo) Returning(value interface{}, columns ...string) IActionInfoDo {
	return a.withDO(a.DO.Returning(value, columns...))
}

func (a actionInfoDo) Not(conds ...gen.Condition) IActionInfoDo {
	return a.withDO(a.DO.Not(conds...))
}

func (a actionInfoDo) Or(conds ...gen.Condition) IActionInfoDo {
	return a.withDO(a.DO.Or(conds...))
}

func (a actionInfoDo) Select(conds ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.Select(conds...))
}

func (a actionInfoDo) Where(conds ...gen.Condition) IActionInfoDo {
	return a.withDO(a.DO.Where(conds...))
}

func (a actionInfoDo) Order(conds ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.Order(conds...))
}

func (a actionInfoDo) Distinct(cols ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.Distinct(cols...))
}

func (a actionInfoDo) Omit(cols ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.Omit(cols...))
}

func (a actionInfoDo) Join(table schema.Tabler, on ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.Join(table, on...))
}

func (a actionInfoDo) LeftJoin(table schema.Tabler, on ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.LeftJoin(table, on...))
}

func (a actionInfoDo) RightJoin(table schema.Tabler, on ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.RightJoin(table, on...))
}

func (a actionInfoDo) Group(cols ...field.Expr) IActionInfoDo {
	return a.withDO(a.DO.Group(cols...))
}

func (a actionInfoDo) Having(conds ...gen.Condition) IActionInfoDo {
	return a.withDO(a.DO.Having(conds...))
}

func (a actionInfoDo) Limit(limit int) IActionInfoDo {
	return a.withDO(a.DO.Limit(limit))
}

func (a actionInfoDo) Offset(offset int) IActionInfoDo {
	return a.withDO(a.DO.Offset(offset))
}

func (a actionInfoDo) Scopes(funcs ...func(gen.Dao) gen.Dao) IActionInfoDo {
	return a.withDO(a.DO.Scopes(funcs...))
}

func (a actionInfoDo) Unscoped() IActionInfoDo {
	return a.withDO(a.DO.Unscoped())
}

func (a actionInfoDo) Create(values ...*model.ActionInfo) error {
	if len(values) == 0 {
		return nil
	}
	return a.DO.Create(values)
}

func (a actionInfoDo) CreateInBatches(values []*model.ActionInfo, batchSize int) error {
	return a.DO.CreateInBatches(values, batchSize)
}

// Save : !!! underlying implementation is different with GORM
// The method is equivalent to executing the statement: db.Clauses(clause.OnConflict{UpdateAll: true}).Create(values)
func (a actionInfoDo) Save(values ...*model.ActionInfo) error {
	if len(values) == 0 {
		return nil
	}
	return a.DO.Save(values)
}

func (a actionInfoDo) First() (*model.ActionInfo, error) {
	if result, err := a.DO.First(); err != nil {
		return nil, err
	} else {
		return result.(*model.ActionInfo), nil
	}
}

func (a actionInfoDo) Take() (*model.ActionInfo, error) {
	if result, err := a.DO.Take(); err != nil {
		return nil, err
	} else {
		return result.(*model.ActionInfo), nil
	}
}

func (a actionInfoDo) Last() (*model.ActionInfo, error) {
	if result, err := a.DO.Last(); err != nil {
		return nil, err
	} else {
		return result.(*model.ActionInfo), nil
	}
}

func (a actionInfoDo) Find() ([]*model.ActionInfo, error) {
	result, err := a.DO.Find()
	return result.([]*model.ActionInfo), err
}

func (a actionInfoDo) FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.ActionInfo, err error) {
	buf := make([]*model.ActionInfo, 0, batchSize)
	err = a.DO.FindInBatches(&buf, batchSize, func(tx gen.Dao, batch int) error {
		defer func() { results = append(results, buf...) }()
		return fc(tx, batch)
	})
	return results, err
}

func (a actionInfoDo) FindInBatches(result *[]*model.ActionInfo, batchSize int, fc func(tx gen.Dao, batch int) error) error {
	return a.DO.FindInBatches(result, batchSize, fc)
}

func (a actionInfoDo) Attrs(attrs ...field.AssignExpr) IActionInfoDo {
	return a.withDO(a.DO.Attrs(attrs...))
}

func (a actionInfoDo) Assign(attrs ...field.AssignExpr) IActionInfoDo {
	return a.withDO(a.DO.Assign(attrs...))
}

func (a actionInfoDo) Joins(fields ...field.RelationField) IActionInfoDo {
	for _, _f := range fields {
		a = *a.withDO(a.DO.Joins(_f))
	}
	return &a
}

func (a actionInfoDo) Preload(fields ...field.RelationField) IActionInfoDo {
	for _, _f := range fields {
		a = *a.withDO(a.DO.Preload(_f))
	}
	return &a
}

func (a actionInfoDo) FirstOrInit() (*model.ActionInfo, error) {
	if result, err := a.DO.FirstOrInit(); err != nil {
		return nil, err
	} else {
		return result.(*model.ActionInfo), nil
	}
}

func (a actionInfoDo) FirstOrCreate() (*model.ActionInfo, error) {
	if result, err := a.DO.FirstOrCreate(); err != nil {
		return nil, err
	} else {
		return result.(*model.ActionInfo), nil
	}
}

func (a actionInfoDo) FindByPage(offset int, limit int) (result []*model.ActionInfo, count int64, err error) {
	result, err = a.Offset(offset).Limit(limit).Find()
	if err != nil {
		return
	}

	if size := len(result); 0 < limit && 0 < size && size < limit {
		count = int64(size + offset)
		return
	}

	count, err = a.Offset(-1).Limit(-1).Count()
	return
}

func (a actionInfoDo) ScanByPage(result interface{}, offset int, limit int) (count int64, err error) {
	count, err = a.Count()
	if err != nil {
		return
	}

	err = a.Offset(offset).Limit(limit).Scan(result)
	return
}

func (a actionInfoDo) Scan(result interface{}) (err error) {
	return a.DO.Scan(result)
}

func (a actionInfoDo) Delete(models ...*model.ActionInfo) (result gen.ResultInfo, err error) {
	return a.DO.Delete(models)
}

func (a *actionInfoDo) withDO(do gen.Dao) *actionInfoDo {
	a.DO = *do.(*gen.DO)
	return a
}