package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/model"
	"project/internal/query"
	"project/internal/repo"
	"project/pkg/errcode"
	"project/pkg/global"
	"project/pkg/utils"
)

func closeoutInstallDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.MarketBundleInstallation{}, &model.MarketResourceMapping{}, &model.MarketBundleBindingStatus{}, &model.MarketInstallationAudit{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX closeout_install_key ON market_bundle_installations(tenant_id,idempotency_key)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX closeout_binding_key ON market_bundle_binding_status(installation_id,binding_key)").Error; err != nil {
		t.Fatal(err)
	}
	old := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = old; pool.Close() })
	return db
}

func closeoutErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var result *errcode.Error
	if !errors.As(err, &result) {
		t.Fatalf("expected structured error, got %v", err)
	}
	data, ok := result.Data.(map[string]interface{})
	if !ok || data["errorCode"] != want {
		t.Fatalf("expected %s, got %#v", want, result.Data)
	}
}

func TestCloseoutInstallationDigest(t *testing.T) {
	base := model.InstallBundleRequest{BundleKey: "b", Version: "1", MarketToken: "secret-a", DeviceBindings: []model.DeviceBindingInput{{BindingKey: "z", LocalDeviceID: "d-z"}, {BindingKey: "a", LocalDeviceID: "d-a"}}}
	digest := installationRequestDigest(&base, "tenant", "actor")
	same := base
	same.MarketToken = "rotated-secret"
	same.IdempotencyKey = "other-transport-key"
	same.OverwritePolicy = "default"
	same.DeviceBindings = []model.DeviceBindingInput{base.DeviceBindings[1], base.DeviceBindings[0]}
	if got := installationRequestDigest(&same, "tenant", "actor"); got != digest {
		t.Fatal("credentials, order or equivalent default changed digest")
	}
	for _, field := range []string{"bundle", "version", "policy", "device", "binding", "tenant", "actor"} {
		t.Run(field, func(t *testing.T) {
			changed := base
			changed.DeviceBindings = append([]model.DeviceBindingInput(nil), base.DeviceBindings...)
			tenant, actor := "tenant", "actor"
			switch field {
			case "bundle":
				changed.BundleKey = "new"
			case "version":
				changed.Version = "2"
			case "policy":
				changed.OverwritePolicy = "upgrade"
			case "device":
				changed.DeviceBindings[0].LocalDeviceID = "new"
			case "binding":
				changed.DeviceBindings[0].BindingKey = "new"
			case "tenant":
				tenant = "other"
			case "actor":
				actor = "other"
			}
			if installationRequestDigest(&changed, tenant, actor) == digest {
				t.Fatal("operation-affecting change was ignored")
			}
		})
	}
	if strings.Contains(digest, "secret") {
		t.Fatal("credential leaked")
	}
}

func TestCloseoutReplayBeforeEffects(t *testing.T) {
	db := closeoutInstallDB(t)
	req := &model.InstallBundleRequest{BundleKey: "b", Version: "1", IdempotencyKey: "stable", MarketToken: "credential"}
	inst := &model.MarketBundleInstallation{ID: "record", TenantID: "t", BundleKey: "b", BundleVersion: "1", IdempotencyKey: "stable", RequestDigest: installationRequestDigest(req, "t", "u"), Status: model.InstallStateCompleted}
	if err := db.Create(inst).Error; err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "unexpected", 500) }))
	defer fake.Close()
	s := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo(), marketClient: &MarketClient{baseURL: fake.URL}, httpClient: fake.Client()}
	claims := &utils.UserClaims{TenantID: "t", ID: "u"}
	for i := 0; i < 100; i++ {
		resp, err := s.InstallBundle(context.Background(), req, claims)
		if err != nil || !resp.IsIdempotent || resp.InstallationID != inst.ID {
			t.Fatalf("replay %d: %#v %v", i, resp, err)
		}
	}
	req.Version = "2"
	_, err := s.InstallBundle(context.Background(), req, claims)
	closeoutErrorCode(t, err, "IDEMPOTENCY_PAYLOAD_CONFLICT")
	req.Version = "1"
	if err := db.Model(inst).Update("request_digest", "").Error; err != nil {
		t.Fatal(err)
	}
	_, err = s.InstallBundle(context.Background(), req, claims)
	closeoutErrorCode(t, err, "IDEMPOTENCY_LEGACY_UNVERIFIABLE")
	if calls.Load() != 0 {
		t.Fatal("replays started external effects")
	}
}

func TestCloseoutStatusFences(t *testing.T) {
	db := closeoutInstallDB(t)
	r := repo.NewMarketBundleInstallRepo()
	ctx := context.Background()
	inst := &model.MarketBundleInstallation{ID: "fence", TenantID: "t", IdempotencyKey: "fence", Status: model.InstallStateVerified}
	if err := db.Create(inst).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.TransitionStatus(ctx, inst.ID, "foreign", model.InstallStateVerified, model.InstallStateModelsInstalled, "", ""); !errors.Is(err, repo.ErrInstallationStateConflict) {
		t.Fatalf("tenant fence: %v", err)
	}
	if err := r.TransitionStatus(ctx, inst.ID, "t", model.InstallStateVerified, model.InstallStateCompensationRequired, "UNKNOWN", "preserve"); err != nil {
		t.Fatal(err)
	}
	if err := r.TransitionStatus(ctx, inst.ID, "t", model.InstallStateVerified, model.InstallStateModelsInstalled, "", ""); !errors.Is(err, repo.ErrInstallationStateConflict) {
		t.Fatalf("stale worker: %v", err)
	}
	if err := r.TransitionStatus(ctx, inst.ID, "t", model.InstallStateCompensationRequired, model.InstallStateDownloading, "", ""); !errors.Is(err, repo.ErrInstallationStateConflict) {
		t.Fatalf("review reset: %v", err)
	}
	var got model.MarketBundleInstallation
	if err := db.First(&got, "id = ?", inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != model.InstallStateCompensationRequired || got.ErrorMessage != "preserve" {
		t.Fatalf("review evidence overwritten: %#v", got)
	}
}

func TestCloseoutFinalStateFailureAndDistinctInstances(t *testing.T) {
	db := closeoutInstallDB(t)
	var downloads, notifications atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download") {
			downloads.Add(1)
			fmt.Fprint(w, `{"contractVersion":"1.0","resources":{},"security":{}}`)
			return
		}
		notifications.Add(1)
		http.Error(w, "unexpected notification", 500)
	}))
	defer fake.Close()
	s := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo(), marketClient: &MarketClient{baseURL: fake.URL}, httpClient: fake.Client()}
	injected := errors.New("injected final state write failure")
	if err := db.Callback().Update().Before("gorm:update").Register("closeout:final_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "market_bundle_installations" {
			if fields, ok := tx.Statement.Dest.(map[string]interface{}); ok && fields["status"] == model.InstallStateCompleted {
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	req := &model.InstallBundleRequest{BundleKey: "b", Version: "1", MarketToken: "secret"}
	for i := 0; i < 2; i++ {
		resp, err := s.InstallBundle(context.Background(), req, &utils.UserClaims{TenantID: "t", ID: "u"})
		if resp != nil {
			t.Fatal("returned completion after failed persistence")
		}
		closeoutErrorCode(t, err, "PERSISTENCE_RESULT_UNKNOWN")
	}
	var installs []model.MarketBundleInstallation
	if err := db.Find(&installs).Error; err != nil {
		t.Fatal(err)
	}
	if len(installs) != 2 || installs[0].IdempotencyKey == installs[1].IdempotencyKey {
		t.Fatalf("legitimate instances merged: %#v", installs)
	}
	for _, inst := range installs {
		if inst.Status != model.InstallStateDashboardsCreated {
			t.Fatalf("unpersisted completion reported: %#v", inst)
		}
	}
	if downloads.Load() != 2 || notifications.Load() != 0 {
		t.Fatalf("unexpected calls: downloads=%d notifications=%d", downloads.Load(), notifications.Load())
	}
}

func TestCloseoutDashboardMappingFailureAndSharedBinding(t *testing.T) {
	for _, failMapping := range []bool{false, true} {
		t.Run(fmt.Sprint(failMapping), func(t *testing.T) {
			db := closeoutInstallDB(t)
			var imports atomic.Int64
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := imports.Add(1)
				fmt.Fprintf(w, `{"dashboardId":"dashboard-%d"}`, n)
			}))
			defer fake.Close()
			s := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo(), thingsVis: &ThingsVisClient{baseURL: fake.URL, internalToken: "fixture-only", httpClient: fake.Client()}}
			if failMapping {
				if err := db.Callback().Create().Before("gorm:create").Register("closeout:mapping_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "market_resource_mappings" {
						tx.AddError(errors.New("mapping unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			bundle := &model.HorizonBundleDownload{Resources: []byte(`{"dashboards":[{"resourceKey":"one","name":"one","deviceBindings":[{"bindingKey":"shared","deviceTemplateKey":"template","required":true}]},{"resourceKey":"two","name":"two","deviceBindings":[{"bindingKey":"shared","deviceTemplateKey":"template","required":true}]}]}`)}
			mappings, err := s.createDashboards(context.Background(), "installation", "tenant", "user", bundle, map[string]string{"shared": "device"})
			if failMapping {
				closeoutErrorCode(t, err, "PERSISTENCE_RESULT_UNKNOWN")
				if imports.Load() != 1 || mappings != nil {
					t.Fatalf("continued after unknown mapping: %d %#v", imports.Load(), mappings)
				}
			} else {
				if err != nil || len(mappings) != 2 {
					t.Fatalf("shared binding rejected: %#v %v", mappings, err)
				}
			}
			var count int64
			if err := db.Model(&model.MarketBundleBindingStatus{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("shared binding duplicated: %d", count)
			}
		})
	}
}

func TestCloseoutRepeatedDownloadPreservesPriorInstallation(t *testing.T) {
	db := closeoutInstallDB(t)
	service := &DashboardTemplateService{installService: &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo()}}
	req := &model.DownloadDashboardTemplateRequest{BundleKey: "bundle", Version: "1"}
	first, err := service.prepareDownloadInstallation(context.Background(), req, "tenant")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(first).Update("status", model.InstallStateCompleted).Error; err != nil {
		t.Fatal(err)
	}
	second, err := service.prepareDownloadInstallation(context.Background(), req, "tenant")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.IdempotencyKey == second.IdempotencyKey {
		t.Fatal("independent downloads share operation identity")
	}
	var persisted model.MarketBundleInstallation
	if err := db.First(&persisted, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.InstallStateCompleted {
		t.Fatal("repeated download reset old worker state")
	}
}

func TestCloseoutBindingIntentFailureStopsImport(t *testing.T) {
	db := closeoutInstallDB(t)
	var imports atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		imports.Add(1)
		fmt.Fprint(w, `{"dashboardId":"unexpected"}`)
	}))
	defer fake.Close()
	s := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo(), thingsVis: &ThingsVisClient{baseURL: fake.URL, internalToken: "fixture", httpClient: fake.Client()}}
	if err := db.Callback().Create().Before("gorm:create").Register("closeout:binding_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "market_bundle_binding_status" {
			tx.AddError(errors.New("binding write failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	bundle := &model.HorizonBundleDownload{Resources: []byte(`{"dashboards":[{"resourceKey":"one","deviceBindings":[{"bindingKey":"binding","deviceTemplateKey":"template","required":true}]}]}`)}
	_, err := s.createDashboards(context.Background(), "i", "t", "u", bundle, map[string]string{"binding": "device"})
	closeoutErrorCode(t, err, "PERSISTENCE_RESULT_UNKNOWN")
	if imports.Load() != 0 {
		t.Fatal("dashboard imported without durable binding intent")
	}
}

func TestCloseoutReplayReadFailureIsNotEmptySuccess(t *testing.T) {
	db := closeoutInstallDB(t)
	req := &model.InstallBundleRequest{BundleKey: "b", Version: "1", IdempotencyKey: "key"}
	inst := &model.MarketBundleInstallation{ID: "i", TenantID: "t", IdempotencyKey: "key", RequestDigest: installationRequestDigest(req, "t", "u"), Status: model.InstallStateCompleted}
	if err := db.Create(inst).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&model.MarketResourceMapping{}); err != nil {
		t.Fatal(err)
	}
	s := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo()}
	resp, err := s.InstallBundle(context.Background(), req, &utils.UserClaims{TenantID: "t", ID: "u"})
	if resp != nil {
		t.Fatal("failed ledger read returned successful empty replay")
	}
	closeoutErrorCode(t, err, "PERSISTENCE_RESULT_UNKNOWN")
}

func TestCloseoutReuseMappingAndFailure(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmt.Sprint(failWrite), func(t *testing.T) {
			db := closeoutInstallDB(t)
			if err := db.AutoMigrate(&model.DeviceTemplate{}); err != nil {
				t.Fatal(err)
			}
			tenantQuery := query.Use(db)
			previous := query.DeviceTemplate
			query.DeviceTemplate = &tenantQuery.DeviceTemplate
			t.Cleanup(func() { query.DeviceTemplate = previous })
			tpl := &model.DeviceTemplate{ID: "preexisting", Name: "existing", TenantID: "tenant"}
			if err := db.Create(tpl).Error; err != nil {
				t.Fatal(err)
			}
			if failWrite {
				if err := db.Callback().Create().Before("gorm:create").Register("closeout:reuse_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "market_resource_mappings" {
						tx.AddError(errors.New("reuse mapping failed"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			s := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo()}
			inst := &model.MarketBundleInstallation{ID: "installation", TenantID: "tenant", IdempotencyKey: "reuse", Status: model.InstallStateFailed}
			if err := db.Create(inst).Error; err != nil {
				t.Fatal(err)
			}
			bundle := &model.HorizonBundleDownload{Resources: []byte(`{"deviceTemplates":[{"resourceKey":"ref","version":"1","name":"existing"}]}`)}
			mappings, _, err := s.installDeviceTemplates(context.Background(), "installation", "tenant", bundle, "default")
			if failWrite {
				closeoutErrorCode(t, err, "PERSISTENCE_RESULT_UNKNOWN")
				if mappings != nil {
					t.Fatal("reuse mapping failure returned successful resource")
				}
				return
			}
			if err != nil || len(mappings) != 1 || mappings[0].Status != "skipped_existing" {
				t.Fatalf("reuse: %#v %v", mappings, err)
			}
			replay, err := s.buildIdempotentResponse(context.Background(), &model.MarketBundleInstallation{ID: "installation", TenantID: "tenant", Status: model.InstallStateCompleted}, "tenant")
			if err != nil || len(replay.ResourceMappings) != 1 || replay.ResourceMappings[0].LocalID != tpl.ID || replay.ResourceMappings[0].Status != "skipped_existing" {
				t.Fatalf("reuse lost at replay: %#v %v", replay, err)
			}
			s.checkCompensationNeeded(context.Background(), "installation", "tenant")
			var saved model.MarketBundleInstallation
			if err := db.First(&saved, "id = ?", inst.ID).Error; err != nil {
				t.Fatal(err)
			}
			if saved.Status != model.InstallStateFailed {
				t.Fatal("reused resource incorrectly treated as owned active effect")
			}
			var retained model.DeviceTemplate
			if err := db.First(&retained, "id = ?", tpl.ID).Error; err != nil {
				t.Fatal("pre-existing resource was removed", err)
			}
		})
	}
}

func TestCloseoutUnsupportedUpgradeStartsNoWork(t *testing.T) {
	db := closeoutInstallDB(t)
	service := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo()}
	_, err := service.InstallBundle(context.Background(), &model.InstallBundleRequest{BundleKey: "b", Version: "2", OverwritePolicy: "upgrade"}, &utils.UserClaims{TenantID: "t", ID: "u"})
	closeoutErrorCode(t, err, "OVERWRITE_POLICY_UNSUPPORTED")
	var count int64
	if err := db.Model(&model.MarketBundleInstallation{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unsupported upgrade created an operation")
	}
	_, _, err = service.installDeviceTemplates(context.Background(), "i", "t", &model.HorizonBundleDownload{}, "upgrade")
	closeoutErrorCode(t, err, "OVERWRITE_POLICY_UNSUPPORTED")
}

func TestCloseoutTemplateCreatePanicRollsBackAndPropagates(t *testing.T) {
	db := closeoutInstallDB(t)
	if err := db.AutoMigrate(&model.DeviceTemplate{}); err != nil {
		t.Fatal(err)
	}
	tenantQuery := query.Use(db)
	previous := query.DeviceTemplate
	query.DeviceTemplate = &tenantQuery.DeviceTemplate
	t.Cleanup(func() { query.DeviceTemplate = previous })
	const panicValue = "injected template creation panic"
	if err := db.Callback().Create().After("gorm:create").Register("closeout:template_panic", func(tx *gorm.DB) {
		if tx.Statement.Table == "device_templates" {
			panic(panicValue)
		}
	}); err != nil {
		t.Fatal(err)
	}
	var recovered interface{}
	func() {
		defer func() { recovered = recover() }()
		service := &MarketBundleInstallService{installRepo: repo.NewMarketBundleInstallRepo()}
		_, _, _ = service.installDeviceTemplates(context.Background(), "installation", "tenant", &model.HorizonBundleDownload{Resources: []byte(`{"deviceTemplates":[{"resourceKey":"new","name":"new"}]}`)}, "default")
	}()
	if recovered != panicValue {
		t.Fatalf("panic became an empty successful result: %#v", recovered)
	}
	var count int64
	if err := db.Model(&model.DeviceTemplate{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("panic left partially created template")
	}
}
