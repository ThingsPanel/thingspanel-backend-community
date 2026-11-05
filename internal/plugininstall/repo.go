package plugininstall

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("installation not found")

// Repo is the GORM store. Leases are taken with a conditional UPDATE (rows affected = 1), which needs no dialect
// specific locking and behaves the same on PostgreSQL and MySQL.
type Repo struct{ db func() *gorm.DB }

func NewRepo(db func() *gorm.DB) *Repo { return &Repo{db: db} }

func (r *Repo) Get(tenant, id string) (*Installation, error) {
	var row Installation
	res := r.db().Where("id = ? AND tenant_id = ?", id, tenant).Limit(1).Find(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &row, nil
}

func (r *Repo) byID(id string) (*Installation, error) {
	var row Installation
	res := r.db().Where("id = ?", id).Limit(1).Find(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &row, nil
}

func (r *Repo) ByPlugin(tenant, plugin string) (*Installation, error) {
	var row Installation
	res := r.db().Where("tenant_id = ? AND plugin_id = ?", tenant, plugin).Limit(1).Find(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return &row, nil
}

func (r *Repo) List(tenant string) ([]Installation, error) {
	var rows []Installation
	return rows, r.db().Where("tenant_id = ?", tenant).Order("created_at").Find(&rows).Error
}

// Create inserts the row; a concurrent insert of the same (tenant, plugin) loses on the unique key and the caller
// re-reads the winner.
func (r *Repo) Create(row *Installation) error { return r.db().Create(row).Error }

func (r *Repo) Save(row *Installation) error {
	row.UpdatedAt = time.Now().UTC()
	return r.db().Save(row).Error
}

// Candidates are rows that may need work now: an operation in flight, or an enabled row whose maintenance is due.
func (r *Repo) Candidates(now time.Time, maintainBefore time.Time) ([]Installation, error) {
	var rows []Installation
	err := r.db().Where("(lease_until IS NULL OR lease_until < ?) AND ((state IN ? ) OR (state = ? AND (maintained_at IS NULL OR maintained_at < ?)))",
		now, []string{StateInstalling, StateUninstalling}, StateEnabled, maintainBefore).Limit(50).Find(&rows).Error
	return rows, err
}

// Claim takes the operation lease; exactly one concurrent caller gets true.
func (r *Repo) Claim(id, owner string, now, until time.Time) (bool, error) {
	res := r.db().Model(&Installation{}).Where("id = ? AND (lease_until IS NULL OR lease_until < ? OR lease_owner = ?)", id, now, owner).
		Updates(map[string]any{"lease_owner": owner, "lease_until": until})
	return res.RowsAffected == 1, res.Error
}

func (r *Repo) Release(id, owner string, until *time.Time) error {
	var value any = gorm.Expr("NULL")
	if until != nil {
		value = *until
	}
	return r.db().Model(&Installation{}).Where("id = ? AND lease_owner = ?", id, owner).
		Updates(map[string]any{"lease_until": value, "lease_owner": ""}).Error
}

func (r *Repo) AddEvent(e *Event) error { return r.db().Create(e).Error }

func (r *Repo) Events(id string, limit int) ([]Event, error) {
	var out []Event
	return out, r.db().Where("installation_id = ?", id).Order("id DESC").Limit(limit).Find(&out).Error
}
