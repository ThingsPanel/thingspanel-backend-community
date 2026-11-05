package plugininstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"project/internal/model"
	"project/internal/pluginidentity"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Identities issues, renews and revokes the managed runtime identity of an instance (internal/pluginidentity).
type Identities interface {
	Issue(tenant, instance, service string, generation int64, ttl time.Duration, now time.Time) (string, error)
	Renew(tenant, instance string, ttl time.Duration, now time.Time) error
	Revoke(tenant, instance string, now time.Time) error
}

var (
	ErrGenerationNotHigher = pluginidentity.ErrGenerationNotHigher
	ErrIdentityNotLive     = errors.New("no live identity to renew")
)

type storeIdentities struct{ s *pluginidentity.Store }

func NewStoreIdentities(s *pluginidentity.Store) Identities { return storeIdentities{s} }

func (i storeIdentities) Issue(tenant, instance, service string, g int64, ttl time.Duration, now time.Time) (string, error) {
	token, _, err := i.s.Issue(tenant, instance, service, g, ttl, now)
	return token, err
}
func (i storeIdentities) Renew(tenant, instance string, ttl time.Duration, now time.Time) error {
	n, err := i.s.Renew(tenant, instance, ttl, now)
	if err == nil && n == 0 {
		return ErrIdentityNotLive
	}
	return err
}
func (i storeIdentities) Revoke(tenant, instance string, now time.Time) error {
	_, err := i.s.Revoke(tenant, instance, now)
	return err
}

// Services registers the plugin in service_plugins (the existing registry the device UI uses).
type Services interface {
	Ensure(ctx context.Context, entry CatalogEntry) (id, outcome string, err error)
}

type gormServices struct{ db func() *gorm.DB }

func NewGormServices(db func() *gorm.DB) Services { return gormServices{db} }

// Ensure looks the row up by service_identifier; it creates it once and afterwards only refreshes the declared
// fields, keeping every other key a user may have added. A repeat call never creates a second row.
func (g gormServices) Ensure(_ context.Context, e CatalogEntry) (string, string, error) {
	spec := e.ServicePlugin
	if spec == nil {
		return "", "skipped", nil
	}
	values := map[string]string{"alias": e.NetworkAlias, "http_port": fmt.Sprint(e.HTTPPort)}
	declared := map[string]string{}
	for k, v := range spec.Config {
		for name, val := range values {
			v = strings.ReplaceAll(v, "{"+name+"}", val)
		}
		declared[k] = v
	}
	now := time.Now().UTC()
	db := g.db()
	var existing []model.ServicePlugin
	if err := db.Where("service_identifier = ?", e.ServiceIdentifier).Order("create_at").Limit(1).Find(&existing).Error; err != nil {
		return "", "", err
	}
	version, desc := e.Version, truncate(spec.Description, 255)
	if len(existing) > 0 {
		row := existing[0]
		merged := map[string]any{}
		if row.ServiceConfig != nil {
			_ = json.Unmarshal([]byte(*row.ServiceConfig), &merged)
		}
		for k, v := range declared {
			merged[k] = v
		}
		raw, _ := json.Marshal(merged)
		cfg := string(raw)
		err := db.Model(&model.ServicePlugin{}).Where("id = ?", row.ID).Updates(map[string]any{
			"service_config": cfg, "version": version, "update_at": now}).Error
		return row.ID, "updated", err
	}
	raw, _ := json.Marshal(declared)
	cfg := string(raw)
	row := model.ServicePlugin{ID: uuid.NewString(), Name: spec.Name, ServiceIdentifier: e.ServiceIdentifier, ServiceType: spec.ServiceType,
		Version: &version, Description: &desc, ServiceConfig: &cfg, CreateAt: now, UpdateAt: now}
	return row.ID, "created", db.Create(&row).Error
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Health asks the plugin's own health endpoint through its stable network alias, the same address the platform uses.
type Health interface {
	Check(ctx context.Context, entry CatalogEntry) error
}

type HTTPHealth struct{ Client *http.Client }

func (h HTTPHealth) Check(ctx context.Context, e CatalogEntry) error {
	if e.Health == nil {
		return nil
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:%d%s", e.NetworkAlias, e.Health.Port, e.Health.Path), nil)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	if res.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(string(body)), "ok") {
		return errors.New("health check did not answer ok")
	}
	return nil
}
