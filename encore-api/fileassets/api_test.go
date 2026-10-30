package fileassets

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFileAssetMultipartStrictAndBounded(t *testing.T) {
	image := []byte("\x89PNG\r\n\x1a\nimage-bytes")
	for _, tc := range []struct {
		name   string
		fields []string
		image  []byte
		want   int
	}{
		{"normal", []string{"file", "purpose"}, image, 0},
		{"reverse order", []string{"purpose", "file"}, image, 0},
		{"duplicate file", []string{"file", "purpose", "file"}, image, 422},
		{"duplicate purpose", []string{"file", "purpose", "purpose"}, image, 422},
		{"unknown field", []string{"file", "purpose", "secret"}, image, 422},
		{"missing purpose", []string{"file"}, image, 422},
		{"missing file", []string{"purpose"}, image, 422},
		{"bad signature", []string{"file", "purpose"}, []byte("<svg>hidden</svg>"), 422},
		{"too large", []string{"file", "purpose"}, append(image, make([]byte, MaxTemplateImageBytes)...), 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for _, name := range tc.fields {
				if name == "file" {
					h := make(textproto.MIMEHeader)
					h.Set("Content-Disposition", `form-data; name="file"; filename="photo.png"`)
					h.Set("Content-Type", "image/png")
					part, err := writer.CreatePart(h)
					require.NoError(t, err)
					_, err = part.Write(tc.image)
					require.NoError(t, err)
				} else {
					value := "template-image"
					if name != "purpose" {
						value = "never-forward-me"
					}
					require.NoError(t, writer.WriteField(name, value))
				}
			}
			require.NoError(t, writer.Close())
			req := httptest.NewRequest(http.MethodPost, createAssetPath, &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			contents, meta, err, status := readAssetMultipart(httptest.NewRecorder(), req)
			require.Equal(t, tc.want, status)
			if tc.want == 0 {
				require.NoError(t, err)
				require.Equal(t, image, contents)
				require.Equal(t, int64(len(image)), meta.SizeBytes)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "never-forward-me")
				require.Empty(t, contents)
			}
		})
	}
	for _, ct := range []string{"application/json", "multipart/form-data"} {
		req := httptest.NewRequest(http.MethodPost, createAssetPath, strings.NewReader("garbage"))
		req.Header.Set("Content-Type", ct)
		_, _, err, status := readAssetMultipart(httptest.NewRecorder(), req)
		require.Error(t, err)
		require.Equal(t, 400, status)
	}
}

func TestFileAssetPrincipalMatchesExistingVerifiedTenantBoundary(t *testing.T) {
	tenant := uuid.NewString()
	for _, tc := range []struct {
		roles []string
		want  errs.ErrCode
	}{
		{[]string{"TENANT_ADMIN"}, errs.OK},
		{[]string{"TENANT_ADMIN", "SYS_ADMIN"}, errs.PermissionDenied},
		{[]string{"SYS_ADMIN"}, errs.PermissionDenied},
		{[]string{"TENANT_USER"}, errs.PermissionDenied},
		{[]string{"tenant_admin"}, errs.PermissionDenied},
	} {
		for _, action := range []string{"read", "write"} {
			principal, err := assetPrincipalFromData(&authn.Data{TenantID: tenant, UserID: "verified-user", Roles: tc.roles, Scopes: []string{"*"}}, action)
			require.Equal(t, tc.want, errs.Code(err))
			if err == nil {
				require.Equal(t, tenant, principal.tenantID)
			}
		}
	}
	_, err := assetPrincipalFromData(nil, "read")
	require.Equal(t, errs.Unauthenticated, errs.Code(err))
	_, err = assetPrincipalFromData(&authn.Data{TenantID: uuid.Nil.String(), UserID: "user", Roles: []string{"TENANT_ADMIN"}}, "read")
	require.Equal(t, errs.Unauthenticated, errs.Code(err))
}

func TestFileAssetConfigHasNoFallbackKeyAndMasksBadConfiguration(t *testing.T) {
	box, err := secretBoxFromConfig("")
	require.NoError(t, err)
	require.Nil(t, box)
	rawKey := bytes.Repeat([]byte("k"), 32)
	cfg := `{"activeKeyId":"current","keys":{"current":"` + base64.StdEncoding.EncodeToString(rawKey) + `"}}`
	box, err = secretBoxFromConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, box)
	for _, bad := range []string{"not-json", `{"activeKeyId":"current","keys":{"current":"private-bad-key"}}`, cfg + " false", `{"extra":"private-value"}`} {
		_, err := secretBoxFromConfig(bad)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private")
	}
}

func TestFileAssetPublicRepresentationOmitsCatalogAndCapabilitiesAreNotCacheable(t *testing.T) {
	asset := FileAsset{ID: uuid.NewString(), ContentType: "image/png", SizeBytes: 5, Checksum: strings.Repeat("a", 64), DownloadURL: "https://objects.test/private?sig=capability"}
	encoded, err := json.Marshal(asset)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.Len(t, fields, 6)
	for _, field := range []string{"tenantId", "objectKey", "purpose", "createdBy"} {
		require.NotContains(t, fields, field)
	}
	first := assetETag(&asset)
	asset.DownloadURL += "-changed"
	require.NotEqual(t, first, assetETag(&asset))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v2/file-assets/id", nil)
	writeAssetProblem(w, r, "request-1", &errs.Error{Code: errs.Internal, Message: "object-storage-secret"}, 0)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
	require.NotContains(t, w.Body.String(), "object-storage-secret")
}
