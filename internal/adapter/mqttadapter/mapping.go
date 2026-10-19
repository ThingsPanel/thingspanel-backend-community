package mqttadapter

import (
	"context"
	"errors"
	"fmt"

	"project/internal/brokermapper"
	"project/internal/model"
	"project/pkg/global"
)

var (
	ErrMappingVersionUnavailable = errors.New("mqtt adapter: mapping version is unavailable")
	ErrMappingCapacity           = errors.New("mqtt adapter: remembered mapping capacity reached")
)

const MaxRememberedMappings = 65536

// initMapper wires the immutable snapshot repository. ConfigureIngressFromConfig
// calls this when ingress_v1 is selected; helpers also check lazily for tests and
// reconnect construction paths.
func (a *Adapter) initMapper() error {
	a.mappingMu.RLock()
	ready := a.mapperRepo != nil
	a.mappingMu.RUnlock()
	if ready {
		return nil
	}
	if global.DB == nil {
		return errors.New("mqtt adapter: database is unavailable for mapping snapshots")
	}
	a.mappingMu.Lock()
	if a.mapperRepo == nil {
		a.mapperRepo = brokermapper.NewRepository(global.DB)
	}
	if a.mappingVersions == nil {
		a.mappingVersions = make(map[string]string)
	}
	a.mappingMu.Unlock()
	return nil
}

func (a *Adapter) mappingRepository() (*brokermapper.Repository, error) {
	if err := a.initMapper(); err != nil {
		return nil, err
	}
	a.mappingMu.RLock()
	repo := a.mapperRepo
	a.mappingMu.RUnlock()
	if repo == nil {
		return nil, errors.New("mqtt adapter: mapping repository is unavailable")
	}
	return repo, nil
}

// mapIngress maps one authenticated device's original wire topic through the
// exact immutable version carried in trusted ingress metadata.
func (a *Adapter) mapIngress(version string, device *model.Device, original string) (string, error) {
	if device == nil || device.DeviceNumber == "" {
		return "", errors.New("mqtt adapter: device mapping scope is missing")
	}
	repo, err := a.mappingRepository()
	if err != nil {
		return "", err
	}
	snapshot, err := repo.Load(context.Background(), version)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMappingVersionUnavailable, err)
	}
	return snapshot.Up(original, device.DeviceNumber)
}

// RememberMapping stores the version only after the ingress owner and envelope
// have been authenticated. It also proves the referenced immutable snapshot is
// present before commands can use it.
func (a *Adapter) RememberMapping(deviceID, version string) error {
	if deviceID == "" || version == "" {
		return errors.New("mqtt adapter: device and mapping version are required")
	}
	repo, err := a.mappingRepository()
	if err != nil {
		return err
	}
	if _, err = repo.Load(context.Background(), version); err != nil {
		return fmt.Errorf("%w: %v", ErrMappingVersionUnavailable, err)
	}
	a.mappingMu.Lock()
	err = a.rememberMappingLocked(deviceID, version)
	a.mappingMu.Unlock()
	return err
}

func (a *Adapter) rememberMappingLocked(deviceID, version string) error {
	if a.mappingVersions == nil {
		a.mappingVersions = make(map[string]string)
	}
	if _, exists := a.mappingVersions[deviceID]; !exists && len(a.mappingVersions) >= MaxRememberedMappings {
		return ErrMappingCapacity
	}
	a.mappingVersions[deviceID] = version
	return nil
}

// InvalidateMapping removes the remembered policy after connection revocation or
// a broker state transition. Callers must use the authenticated device ID.
func (a *Adapter) InvalidateMapping(deviceID string) {
	if deviceID == "" {
		return
	}
	a.mappingMu.Lock()
	delete(a.mappingVersions, deviceID)
	a.mappingMu.Unlock()
}

func (a *Adapter) rememberedMapping(deviceID string) (string, bool) {
	a.mappingMu.RLock()
	defer a.mappingMu.RUnlock()
	version, ok := a.mappingVersions[deviceID]
	return version, ok
}

// mapDownlink requires a unique device number and a version previously accepted
// for that device. It never recompiles live DB rules on the publish path.
func (a *Adapter) mapDownlink(deviceNumber, canonical string, payload []byte) (string, []byte, error) {
	if a.inputProfile != "ingress_v1" {
		return canonical, payload, nil
	}
	if global.DB == nil {
		return "", nil, errors.New("mqtt adapter: database is unavailable")
	}
	var matches []struct {
		ID       string `gorm:"column:id"`
		TenantID string `gorm:"column:tenant_id"`
		Total    int64  `gorm:"column:total"`
	}
	err := global.DB.Raw(`SELECT d.id, d.tenant_id, count(*) OVER() AS total
		FROM devices d LEFT JOIN device_configs c ON c.id = d.device_config_id AND c.tenant_id = d.tenant_id
		WHERE d.device_number = ? AND (d.device_config_id IS NULL OR c.id IS NOT NULL)
		ORDER BY d.id LIMIT 2`, deviceNumber).Scan(&matches).Error
	if err != nil {
		return "", nil, errors.New("mqtt adapter: device lookup failed")
	}
	if len(matches) != 1 || matches[0].Total != 1 {
		count := int64(len(matches))
		if len(matches) > 0 {
			count = matches[0].Total
		}
		return "", nil, fmt.Errorf("mqtt adapter: device number resolves to %d records", count)
	}
	deviceID := matches[0].ID
	if deviceID == "" || matches[0].TenantID == "" {
		return "", nil, errors.New("mqtt adapter: tenant-scoped device identity is unavailable")
	}
	version, ok := a.rememberedMapping(deviceID)
	if !ok {
		return "", nil, ErrMappingVersionUnavailable
	}
	repo, err := a.mappingRepository()
	if err != nil {
		return "", nil, err
	}
	snapshot, err := repo.Load(context.Background(), version)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrMappingVersionUnavailable, err)
	}
	return mapDownlinkWithSnapshot(snapshot, deviceNumber, canonical, payload)
}

func mapDownlinkWithSnapshot(snapshot *brokermapper.Snapshot, deviceNumber, canonical string, payload []byte) (string, []byte, error) {
	if snapshot == nil || deviceNumber == "" {
		return "", nil, errors.New("mqtt adapter: mapping scope is missing")
	}
	return snapshot.Down(canonical, deviceNumber, payload)
}
