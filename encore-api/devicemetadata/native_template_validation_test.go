package devicemetadata

import (
	"context"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestValidationPointer(t *testing.T) {
	for in, want := range map[string]string{
		"configuration.topicMappings": "/configuration/topicMappings",
		"configuration":               "/configuration",
		"$":                           "/configuration",
		"configuration.access.mode":   "/configuration/access/mode",
	} {
		if got := validationPointer(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestNativeTemplateValidationReportsWithoutSaving(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	identity := nativeTemplateWriteTestIdentity(tenantID)
	created, err := createNativeTemplate(ctx, identity, "template-val-create-"+uuid.NewString(), nativeTemplateWriteTestBody(t, "validate-"+uuid.NewString(), nativeTemplateMQTTConfiguration), "val-create-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	template := nativeTemplateWriteTestResult(t, created)

	good, err := validateNativeTemplateConfigurationReport(ctx, identity, template.ID, []byte(nativeTemplateMQTTConfiguration), validateNativeDeviceConfiguration)
	if err != nil || !good.Valid || len(good.Issues) != 0 || len(good.SourceDigest) != 64 {
		t.Fatalf("valid configuration: %+v %v", good, err)
	}

	// A mapping that points at a field the (absent) model does not have is reported, not thrown.
	bad := strings.Replace(nativeTemplateMQTTConfiguration, `"topicMappings":[]`, `"topicMappings":[{"id":"`+uuid.NewString()+`","name":"m","direction":"up","sourceTopic":"a/b","targetTopic":"c","dataKind":"telemetry","dataIdentifier":"missing","priority":100,"description":null,"enabled":true}]`, 1)
	report, err := validateNativeTemplateConfigurationReport(ctx, identity, template.ID, []byte(bad), validateNativeDeviceConfiguration)
	if err != nil || report.Valid || len(report.Issues) == 0 || report.Issues[0].Severity != "error" || !strings.HasPrefix(report.Issues[0].Pointer, "/configuration") {
		t.Fatalf("invalid configuration: %+v %v", report, err)
	}
	if report.SourceDigest == good.SourceDigest {
		t.Fatal("digest must follow the validated content")
	}

	malformed, err := validateNativeTemplateConfigurationReport(ctx, identity, template.ID, []byte(`{"unknown":1}`), validateNativeDeviceConfiguration)
	if err != nil || malformed.Valid || len(malformed.Issues) != 1 {
		t.Fatalf("malformed configuration: %+v %v", malformed, err)
	}

	// Nothing was saved, and a template the caller cannot see answers NotFound.
	after, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantID}, template.ID)
	if err != nil || after.ResourceVersion != template.ResourceVersion {
		t.Fatalf("validation must not change the template: %+v %v", after, err)
	}
	if _, err := validateNativeTemplateConfigurationReport(ctx, nativeTemplateWriteTestIdentity(testTenantID()), template.ID, []byte(nativeTemplateMQTTConfiguration), validateNativeDeviceConfiguration); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant: %v", err)
	}
}
