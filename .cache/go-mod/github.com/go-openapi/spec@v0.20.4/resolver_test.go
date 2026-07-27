package spec

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-openapi/jsonpointer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRef(t *testing.T) {
	var root interface{}
	require.NoError(t, json.Unmarshal([]byte(PetStore20), &root))

	ref, err := NewRef("#/definitions/Category")
	require.NoError(t, err)

	sch, err := ResolveRef(root, &ref)
	require.NoError(t, err)

	b, err := sch.MarshalJSON()
	require.NoError(t, err)

	assert.JSONEq(t, `{"id":"Category","properties":{"id":{"type":"integer","format":"int64"},"name":{"type":"string"}}}`, string(b))

	// WithBase variant
	sch, err = ResolveRefWithBase(root, &ref, &ExpandOptions{
		RelativeBase: "/",
	})
	require.NoError(t, err)

	b, err = sch.MarshalJSON()
	require.NoError(t, err)

	assert.JSONEq(t, `{"id":"Category","properties":{"id":{"type":"integer","format":"int64"},"name":{"type":"string"}}}`, string(b))
}

func TestResolveResponse(t *testing.T) {
	specDoc, err := jsonDoc(filepath.Join("fixtures", "expansion", "all-the-things.json"))
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(specDoc, spec))

	// Resolve with root version
	resp := spec.Paths.Paths["/"].Get.Responses.StatusCodeResponses[200]
	resp2, err := ResolveResponse(spec, resp.Ref)
	require.NoError(t, err)

	// resolve resolves the ref, but dos not expand
	jazon := asJSON(t, resp2)

	assert.JSONEq(t, `{
         "$ref": "#/responses/petResponse"
        }`, jazon)
}

func TestResolveResponseWithBase(t *testing.T) {
	specDoc, err := jsonDoc(crossFileRefFixture)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(specDoc, spec))

	// Resolve with root version
	resp := spec.Paths.Paths["/"].Get.Responses.StatusCodeResponses[200]
	resp2, err := ResolveResponseWithBase(spec, resp.Ref, &ExpandOptions{RelativeBase: crossFileRefFixture})
	require.NoError(t, err)

	// resolve resolves the ref, but dos not expand
	jazon := asJSON(t, resp2)

	assert.JSONEq(t, `{
         "$ref": "#/responses/petResponse"
        }`, jazon)
}

func TestResolveParam(t *testing.T) {
	specDoc, err := jsonDoc(filepath.Join("fixtures", "expansion", "all-the-things.json"))
	require.NoError(t, err)

	var spec Swagger
	require.NoError(t, json.Unmarshal(specDoc, &spec))

	param := spec.Paths.Paths["/pets/{id}"].Get.Parameters[0]
	par, err := ResolveParameter(spec, param.Ref)
	require.NoError(t, err)

	jazon := asJSON(t, par)

	assert.JSONEq(t, `{
      "name": "id",
      "in": "path",
      "description": "ID of pet to fetch",
      "required": true,
      "type": "integer",
      "format": "int64"
      }`, jazon)
}

func TestResolveParamWithBase(t *testing.T) {
	specDoc, err := jsonDoc(crossFileRefFixture)
	require.NoError(t, err)

	var spec Swagger
	require.NoError(t, json.Unmarshal(specDoc, &spec))

	param := spec.Paths.Paths["/pets"].Get.Parameters[0]
	par, err := ResolveParameterWithBase(spec, param.Ref, &ExpandOptions{RelativeBase: crossFileRefFixture})
	require.NoError(t, err)

	jazon := asJSON(t, par)

	assert.JSONEq(t, `{
"description":"ID of pet to fetch",
"format":"int64",
"in":"path",
"name":"id",
"required":true,
"type":"integer"
}`, jazon)
}

func TestResolveRemoteRef_RootSame(t *testing.T) {
	fileserver := http.FileServer(http.Dir(specs))
	server := httptest.NewServer(fileserver)
	defer server.Close()

	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, rootDoc))

	// the filename doesn't matter because ref will eventually point to refed.json
	specBase := normalizeBase(filepath.Join(specs, "anyotherfile.json"))

	var result0 Swagger
	ref0, _ := NewRef(server.URL + "/refed.json#")
	resolver0 := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver0.Resolve(&ref0, &result0, ""))
	assertSpecs(t, result0, *rootDoc)

	var result1 Swagger
	ref1, _ := NewRef("./refed.json")
	resolver1 := defaultSchemaLoader(rootDoc, &ExpandOptions{
		RelativeBase: specBase,
	}, nil, nil)
	require.NoError(t, resolver1.Resolve(&ref1, &result1, specBase))
	assertSpecs(t, result1, *rootDoc)
}

func TestResolveRemoteRef_FromFragment(t *testing.T) {
	fileserver := http.FileServer(http.Dir(specs))
	server := httptest.NewServer(fileserver)
	defer server.Close()

	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt Schema
	ref, err := NewRef(server.URL + "/refed.json#/definitions/pet")
	require.NoError(t, err)

	context := newResolverContext(&ExpandOptions{PathLoader: jsonDoc})
	resolver := &schemaLoader{root: rootDoc, cache: defaultResolutionCache(), context: context}
	require.NoError(t, resolver.Resolve(&ref, &tgt, ""))
	assert.Equal(t, []string{"id", "name"}, tgt.Required)
}

func TestResolveRemoteRef_FromInvalidFragment(t *testing.T) {
	fileserver := http.FileServer(http.Dir(specs))
	server := httptest.NewServer(fileserver)
	defer server.Close()

	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt Schema
	ref, err := NewRef(server.URL + "/refed.json#/definitions/NotThere")
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	assert.Error(t, resolver.Resolve(&ref, &tgt, ""))
}

/* This next test will have to wait until we do full $ID analysis for every subschema on every file that is referenced */
/* For now, TestResolveRemoteRef_WithNestedResolutionContext replaces this next test */
// func TestResolveRemoteRef_WithNestedResolutionContextWithFragment_WithParentID(t *testing.T) {
// 	server := resolutionContextServer()
// 	defer server.Close()
//
// 	rootDoc := new(Swagger)
// 	b, err := ioutil.ReadFile("fixtures/specs/refed.json")
// 	require.NoError(t, err) && assert.NoError(t, json.Unmarshal(b, rootDoc))
//
//	var tgt Schema
// 	ref, err := NewRef(server.URL + "/resolution2.json#/items/items")
// 	require.NoError(t, err)
//
// 	resolver := defaultSchemaLoader(rootDoc, nil, nil,nil)
// 	require.NoError(t, resolver.Resolve(&ref, &tgt, ""))
// 	assert.Equal(t, StringOrArray([]string{"file"}), tgt.Type)
// }

func TestResolveRemoteRef_ToParameter(t *testing.T) {
	fileserver := http.FileServer(http.Dir(specs))
	server := httptest.NewServer(fileserver)
	defer server.Close()

	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt Parameter
	ref, err := NewRef(server.URL + "/refed.json#/parameters/idParam")
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, &tgt, ""))

	assert.Equal(t, "id", tgt.Name)
	assert.Equal(t, "path", tgt.In)
	assert.Equal(t, "ID of pet to fetch", tgt.Description)
	assert.True(t, tgt.Required)
	assert.Equal(t, "integer", tgt.Type)
	assert.Equal(t, "int64", tgt.Format)
}

func TestResolveRemoteRef_ToPathItem(t *testing.T) {
	fileserver := http.FileServer(http.Dir(specs))
	server := httptest.NewServer(fileserver)
	defer server.Close()

	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt PathItem
	ref, err := NewRef(server.URL + "/refed.json#/paths/" + jsonpointer.Escape("/pets/{id}"))
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, &tgt, ""))
	assert.Equal(t, rootDoc.Paths.Paths["/pets/{id}"].Get, tgt.Get)
}

func TestResolveRemoteRef_ToResponse(t *testing.T) {
	fileserver := http.FileServer(http.Dir(specs))
	server := httptest.NewServer(fileserver)
	defer server.Close()

	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt Response
	ref, err := NewRef(server.URL + "/refed.json#/responses/petResponse")
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, &tgt, ""))
	assert.Equal(t, rootDoc.Responses["petResponse"], tgt)
}

func TestResolveLocalRef_SameRoot(t *testing.T) {
	rootDoc := new(Swagger)
	require.NoError(t, json.Unmarshal(PetStoreJSONMessage, rootDoc))

	result := new(Swagger)
	ref, _ := NewRef("#")
	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, result, ""))
	assert.Equal(t, rootDoc, result)
}

func TestResolveLocalRef_FromFragment(t *testing.T) {
	rootDoc := new(Swagger)
	require.NoError(t, json.Unmarshal(PetStoreJSONMessage, rootDoc))

	var tgt Schema
	ref, err := NewRef("#/definitions/Category")
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, &tgt, ""))
	assert.Equal(t, "Category", tgt.ID)
}

func TestResolveLocalRef_FromInvalidFragment(t *testing.T) {
	rootDoc := new(Swagger)
	require.NoError(t, json.Unmarshal(PetStoreJSONMessage, rootDoc))

	var tgt Schema
	ref, err := NewRef("#/definitions/NotThere")
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.Error(t, resolver.Resolve(&ref, &tgt, ""))
}

func TestResolveLocalRef_Parameter(t *testing.T) {
	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)

	basePath := filepath.Join(specs, "refed.json")
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt Parameter
	ref, err := NewRef("#/parameters/idParam")
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, &tgt, basePath))

	assert.Equal(t, "id", tgt.Name)
	assert.Equal(t, "path", tgt.In)
	assert.Equal(t, "ID of pet to fetch", tgt.Description)
	assert.True(t, tgt.Required)
	assert.Equal(t, "integer", tgt.Type)
	assert.Equal(t, "int64", tgt.Format)
}

func TestResolveLocalRef_PathItem(t *testing.T) {
	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)

	basePath := filepath.Join(specs, "refed.json")
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt PathItem
	ref, err := NewRef("#/paths/" + jsonpointer.Escape("/pets/{id}"))
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, &tgt, basePath))
	assert.Equal(t, rootDoc.Paths.Paths["/pets/{id}"].Get, tgt.Get)
}

func TestResolveLocalRef_Response(t *testing.T) {
	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join(specs, "refed.json"))
	require.NoError(t, err)

	basePath := filepath.Join(specs, "refed.json")
	require.NoError(t, json.Unmarshal(b, rootDoc))

	var tgt Response
	ref, err := NewRef("#/responses/petResponse")
	require.NoError(t, err)

	resolver := defaultSchemaLoader(rootDoc, nil, nil, nil)
	require.NoError(t, resolver.Resolve(&ref, &tgt, basePath))
	assert.Equal(t, rootDoc.Responses["petResponse"], tgt)
}

func TestResolvePathItem(t *testing.T) {
	spec := new(Swagger)
	specDoc, err := jsonDoc(pathItemsFixture)
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal(specDoc, spec))

	// Resolve use case
	pth := spec.Paths.Paths["/todos"]
	pathItem, err := ResolvePathItem(spec, pth.Ref, &ExpandOptions{RelativeBase: pathItemsFixture})
	require.NoError(t, err)

	jazon := asJSON(t, pathItem)

	assert.JSONEq(t, `{
         "get": {
          "responses": {
           "200": {
            "description": "List Todos",
            "schema": {
             "type": "array",
             "items": {
              "type": "string"
             }
            }
           },
           "404": {
            "description": "error"
           }
          }
         }
			 }`, jazon)
}

func TestResolveExtraItem(t *testing.T) {
	// go-openapi extra goodie: $ref in simple schema Items and Headers
	spec := new(Swagger)
	specDoc, err := jsonDoc(extraRefFixture)
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal(specDoc, spec))

	// Resolve param Items use case: here we explicitly resolve the unsuppord case
	parm := spec.Paths.Paths["/employees"].Get.Parameters[0]
	parmItem, err := ResolveItems(spec, parm.Items.Ref, &ExpandOptions{RelativeBase: extraRefFixture})
	require.NoError(t, err)

	jazon := asJSON(t, parmItem)

	assert.JSONEq(t, `{
         "type": "integer",
         "format": "int32"
			 }`, jazon)

	// Resolve header Items use case: here we explicitly resolve the unsuppord case
	hdr := spec.Paths.Paths["/employees"].Get.Responses.StatusCodeResponses[200].Headers["X-header"]
	hdrItem, err := ResolveItems(spec, hdr.Items.Ref, &ExpandOptions{RelativeBase: extraRefFixture})
	require.NoError(t, err)

	jazon = asJSON(t, hdrItem)

	assert.JSONEq(t, `{
         "type": "string",
         "format": "uuid"
			 }`, jazon)
}
