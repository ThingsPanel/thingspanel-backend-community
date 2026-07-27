// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spec

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	crossFileRefFixture = "fixtures/expansion/crossFileRef.json"
	withoutSchemaID     = "removed"
	withSchemaID        = "schema"
	pathItemsFixture    = "fixtures/expansion/pathItems.json"
	extraRefFixture     = "fixtures/expansion/extraRef.json"
)

var (
	// PetStoreJSONMessage json raw message for Petstore20
	PetStoreJSONMessage = json.RawMessage([]byte(PetStore20))
	specs               = filepath.Join("fixtures", "specs")
)

func TestExpand_KnownRef(t *testing.T) {
	// json schema draft 4 meta schema is embedded by default: it resolves without setup
	schema := RefProperty("http://json-schema.org/draft-04/schema#")
	require.NoError(t, ExpandSchema(schema, nil, nil))

	assert.Equal(t, "Core schema meta-schema", schema.Description)

	// from the expanded schema, verify that all remaining $ref actually resolve
	jazon := asJSON(t, schema)

	assertRefResolve(t, jazon, "", schema)
}

func TestExpand_ResponseSchema(t *testing.T) {
	fp := filepath.Join("fixtures", "local_expansion", "spec.json")
	b, err := jsonDoc(fp)
	require.NoError(t, err)

	var spec Swagger
	require.NoError(t, json.Unmarshal(b, &spec))

	require.NoError(t, ExpandSpec(&spec, &ExpandOptions{RelativeBase: fp}))

	// verify that the document is full expanded
	jazon := asJSON(t, spec)
	assertNoRef(t, jazon)

	sch := spec.Paths.Paths["/item"].Get.Responses.StatusCodeResponses[200].Schema
	require.NotNil(t, sch)

	assert.Empty(t, sch.Ref.String())
	assert.Contains(t, sch.Type, "object")
	assert.Len(t, sch.Properties, 2)
}

func TestExpand_EmptySpec(t *testing.T) {
	spec := new(Swagger)

	// expansion of an empty spec
	require.NoError(t, ExpandSpec(spec, nil))

	// expansion of a nil schema
	var schema *Schema
	require.NoError(t, ExpandSchemaWithBasePath(schema, nil, nil))

	// expansion of a nil paths
	var paths *PathItem
	resolver := defaultSchemaLoader(spec, nil, nil, nil)
	require.NoError(t, expandPathItem(paths, resolver, ""))

	// expansion of a nil Parameter
	var param *Parameter
	require.NoError(t, expandParameterOrResponse(param, resolver, ""))
}

func TestExpand_Spec(t *testing.T) {

	// expansion of a rich spec
	specPath := filepath.Join("fixtures", "expansion", "all-the-things.json")
	specDoc, err := jsonDoc(specPath)
	require.NoError(t, err)

	opts := &ExpandOptions{
		RelativeBase: specPath,
	}

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(specDoc, spec))

	// verify the resulting unmarshaled structure
	pet := spec.Definitions["pet"]
	errorModel := spec.Definitions["errorModel"]
	petResponse := spec.Responses["petResponse"]
	petResponse.Schema = &pet
	stringResponse := spec.Responses["stringResponse"]
	tagParam := spec.Parameters["tag"]
	idParam := spec.Parameters["idParam"]

	require.NoError(t, ExpandSpec(spec, opts))

	// verify that the spec is fully expanded
	assertNoRef(t, asJSON(t, spec))

	assert.Equal(t, tagParam, spec.Parameters["query"])
	assert.Equal(t, petResponse, spec.Responses["petResponse"])
	assert.Equal(t, petResponse, spec.Responses["anotherPet"])
	assert.Equal(t, pet, *spec.Responses["petResponse"].Schema)
	assert.Equal(t, stringResponse, *spec.Paths.Paths["/"].Get.Responses.Default)
	assert.Equal(t, petResponse, spec.Paths.Paths["/"].Get.Responses.StatusCodeResponses[200])
	assert.Equal(t, pet, *spec.Paths.Paths["/pets"].Get.Responses.StatusCodeResponses[200].Schema.Items.Schema)
	assert.Equal(t, errorModel, *spec.Paths.Paths["/pets"].Get.Responses.Default.Schema)
	assert.Equal(t, pet, spec.Definitions["petInput"].AllOf[0])
	assert.Equal(t, spec.Definitions["petInput"], *spec.Paths.Paths["/pets"].Post.Parameters[0].Schema)
	assert.Equal(t, petResponse, spec.Paths.Paths["/pets"].Post.Responses.StatusCodeResponses[200])
	assert.Equal(t, errorModel, *spec.Paths.Paths["/pets"].Post.Responses.Default.Schema)

	pi := spec.Paths.Paths["/pets/{id}"]
	assert.Equal(t, idParam, pi.Get.Parameters[0])
	assert.Equal(t, petResponse, pi.Get.Responses.StatusCodeResponses[200])
	assert.Equal(t, errorModel, *pi.Get.Responses.Default.Schema)
	assert.Equal(t, idParam, pi.Delete.Parameters[0])
	assert.Equal(t, errorModel, *pi.Delete.Responses.Default.Schema)
}

func TestExpand_InternalResponse(t *testing.T) {
	basePath := normalizeBase(filepath.Join("fixtures", "expansion", "all-the-things.json"))
	specDoc, err := jsonDoc(basePath)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(specDoc, spec))

	resolver := defaultSchemaLoader(spec, nil, nil, nil)

	expectedPet := spec.Responses["petResponse"]
	require.NoError(t, expandParameterOrResponse(&expectedPet, resolver, basePath))

	jazon := asJSON(t, expectedPet)

	assert.JSONEq(t, `{
         "description": "pet response",
         "schema": {
          "required": [
           "id",
           "name"
          ],
          "properties": {
           "id": {
            "type": "integer",
            "format": "int64"
           },
           "name": {
            "type": "string"
           },
           "tag": {
            "type": "string"
           }
          }
         }
			 }`, jazon)

	// response pointing to the same target: result is unchanged
	another := spec.Responses["anotherPet"]
	require.NoError(t, expandParameterOrResponse(&another, resolver, basePath))
	assert.Equal(t, expectedPet, another)

	defaultResponse := spec.Paths.Paths["/"].Get.Responses.Default

	require.NoError(t, expandParameterOrResponse(defaultResponse, resolver, basePath))

	expectedString := spec.Responses["stringResponse"]
	assert.Equal(t, expectedString, *defaultResponse)

	// cascading ref
	successResponse := spec.Paths.Paths["/"].Get.Responses.StatusCodeResponses[200]

	jazon = asJSON(t, successResponse)

	assert.JSONEq(t, `{
		"$ref": "#/responses/anotherPet"
  }`, jazon)

	require.NoError(t, expandParameterOrResponse(&successResponse, resolver, basePath))
	assert.Equal(t, expectedPet, successResponse)
}

func TestExpand_Response(t *testing.T) {
	basePath := filepath.Join("fixtures", "expansion", "all-the-things.json")

	specDoc, err := jsonDoc(basePath)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(specDoc, spec))

	resp := spec.Responses["anotherPet"]
	expected := spec.Responses["petResponse"]

	require.NoError(t, ExpandResponse(&expected, basePath))

	require.NoError(t, ExpandResponse(&resp, basePath))
	assert.Equal(t, expected, resp)

	resp2 := spec.Paths.Paths["/"].Get.Responses.Default
	expected = spec.Responses["stringResponse"]

	require.NoError(t, ExpandResponse(resp2, basePath))
	assert.Equal(t, expected, *resp2)

	resp = spec.Paths.Paths["/"].Get.Responses.StatusCodeResponses[200]
	expected = spec.Responses["petResponse"]

	require.NoError(t, ExpandResponse(&resp, basePath))
	assert.Equal(t, expected, resp)
}

func TestExpand_ResponseAndParamWithRoot(t *testing.T) {
	specDoc, err := jsonDoc("fixtures/bugs/1614/gitea.json")
	require.NoError(t, err)

	var spec Swagger
	err = json.Unmarshal(specDoc, &spec)
	require.NoError(t, err)

	// check responses with $ref
	resp := spec.Paths.Paths["/admin/users"].Post.Responses.StatusCodeResponses[201]
	require.NoError(t, ExpandResponseWithRoot(&resp, spec, nil))

	jazon := asJSON(t, resp)
	assertNoRef(t, jazon)

	resp = spec.Paths.Paths["/admin/users"].Post.Responses.StatusCodeResponses[403]
	require.NoError(t, ExpandResponseWithRoot(&resp, spec, nil))

	jazon = asJSON(t, resp)
	assertNoRef(t, jazon)

	// check param with $ref
	param := spec.Paths.Paths["/admin/users"].Post.Parameters[0]
	require.NoError(t, ExpandParameterWithRoot(&param, spec, nil))

	jazon = asJSON(t, param)
	assertNoRef(t, jazon)
}

func TestExpand_InternalParameter(t *testing.T) {
	basePath := normalizeBase(filepath.Join("fixtures", "expansion", "params.json"))

	paramDoc, err := jsonDoc(basePath)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(paramDoc, spec))

	resolver := defaultSchemaLoader(spec, nil, nil, nil)

	param := spec.Parameters["query"]
	expected := spec.Parameters["tag"]

	require.NoError(t, expandParameterOrResponse(&param, resolver, basePath))

	assert.Equal(t, expected, param)

	param = spec.Paths.Paths["/cars/{id}"].Parameters[0]
	expected = spec.Parameters["id"]

	require.NoError(t, expandParameterOrResponse(&param, resolver, basePath))

	assert.Equal(t, expected, param)
}

func TestExpand_Parameter(t *testing.T) {
	basePath := filepath.Join("fixtures", "expansion", "params.json")

	paramDoc, err := jsonDoc(basePath)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(paramDoc, spec))

	param := spec.Parameters["query"]
	expected := spec.Parameters["tag"]

	require.NoError(t, ExpandParameter(&param, basePath))
	assert.Equal(t, expected, param)

	param = spec.Paths.Paths["/cars/{id}"].Parameters[0]
	expected = spec.Parameters["id"]

	require.NoError(t, ExpandParameter(&param, basePath))
	assert.Equal(t, expected, param)
}

func TestExpand_JSONSchemaDraft4(t *testing.T) {
	fixturePath := filepath.Join("schemas", "jsonschema-draft-04.json")
	jazon, sch := expandThisSchemaOrDieTrying(t, fixturePath)

	// expansion leaves some circular $ref behind: let's assert those

	// assert all $ref match
	// "$ref": "http://json-schema.org/draft-04/something"
	//
	// Case: Circular $refs against schema ID
	assertRefInJSONRegexp(t, jazon, `^(http://json-schema.org/draft-04/)|(#/definitions/schemaArray)`)

	// verify that all remaining $ref may be expanded in the new root schema
	assertRefExpand(t, jazon, "", sch)
}

func TestExpand_SwaggerSchema(t *testing.T) {
	fixturePath := filepath.Join("schemas", "v2", "schema.json")
	jazon, sch := expandThisSchemaOrDieTrying(t, fixturePath)

	// expansion leaves some circular $ref behind: let's assert those

	// assert all $ref match
	// "$ref": "#/definitions/something"
	assertRefInJSON(t, jazon, "#/definitions/")

	// verify that all $ref resolve in the new root schema
	assertRefExpand(t, jazon, "", sch)
}

func TestExpand_ContinueOnError(t *testing.T) {
	specPath := filepath.Join("fixtures", "expansion", "missingRef.json")

	defer log.SetOutput(os.Stdout)
	log.SetOutput(ioutil.Discard)

	// missing $ref in spec
	missingRefDoc, err := jsonDoc(specPath)
	assert.NoError(t, err)

	testCase := struct {
		Input    *Swagger `json:"input"`
		Expected *Swagger `json:"expected"`
	}{}
	require.NoError(t, json.Unmarshal(missingRefDoc, &testCase))

	opts := &ExpandOptions{
		ContinueOnError: true,
		RelativeBase:    specPath,
	}
	require.NoError(t, ExpandSpec(testCase.Input, opts))

	assert.Equal(t, testCase.Input, testCase.Expected, "Should continue expanding spec when a definition can't be found.")

	// missing $ref in items
	doc, err := jsonDoc("fixtures/expansion/missingItemRef.json")
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(doc, spec))

	assert.NotPanics(t, func() {
		require.NoError(t, ExpandSpec(spec, opts))
	}, "Array of missing refs should not cause a panic, and continue to expand spec.")
}

func TestExpand_InternalSchemas2(t *testing.T) {
	basePath := normalizeBase(filepath.Join("fixtures", "expansion", "schemas2.json"))

	carsDoc, err := jsonDoc(basePath)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(carsDoc, spec))

	resolver := defaultSchemaLoader(spec, nil, nil, nil)

	// verify unmarshaled structure
	schema := spec.Definitions["car"]
	oldBrand := schema.Properties["brand"]
	require.NotEmpty(t, oldBrand.Items.Schema.Ref.String()) // this is a $ref
	require.NotEqual(t, spec.Definitions["brand"], oldBrand)

	_, err = expandSchema(schema, []string{"#/definitions/car"}, resolver, basePath)
	require.NoError(t, err)

	// verify expanded schema for Car, in the document passed
	newBrand := schema.Properties["brand"]
	assert.Empty(t, newBrand.Items.Schema.Ref.String())
	assert.Equal(t, spec.Definitions["brand"], *newBrand.Items.Schema)

	// verify expanded schema for Truck, in the returned schema
	schema = spec.Definitions["truck"]
	require.NotEmpty(t, schema.Items.Schema.Ref.String())
	s, err := expandSchema(schema, []string{"#/definitions/truck"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Items.Schema.Ref.String()) // no more a $ref
	assert.False(t, schema.Items.Schema.Ref.IsRoot()) // no more a $ref
	assert.Equal(t, spec.Definitions["car"], *schema.Items.Schema)

	sch := new(Schema)
	_, err = expandSchema(*sch, []string{""}, resolver, basePath)
	require.NoError(t, err)

	// verify expanded schema for Batch, in the returned schema
	schema = spec.Definitions["batch"]
	s, err = expandSchema(schema, []string{"#/definitions/batch"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Items.Schema.Items.Schema.Ref.String())
	assert.Equal(t, *schema.Items.Schema.Items.Schema, spec.Definitions["brand"])

	// verify expanded schema for Batch2, in the returned schema
	schema = spec.Definitions["batch2"]
	s, err = expandSchema(schema, []string{"#/definitions/batch2"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Items.Schemas[0].Items.Schema.Ref.String())
	assert.Empty(t, schema.Items.Schemas[1].Items.Schema.Ref.String())
	assert.Equal(t, *schema.Items.Schemas[0].Items.Schema, spec.Definitions["brand"])
	assert.Equal(t, *schema.Items.Schemas[1].Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for AllOfBoth, in the returned schema [expand allOf]
	schema = spec.Definitions["allofBoth"]
	s, err = expandSchema(schema, []string{"#/definitions/allofBoth"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AllOf[0].Items.Schema.Ref.String())
	assert.Empty(t, schema.AllOf[1].Items.Schema.Ref.String())
	assert.Equal(t, *schema.AllOf[0].Items.Schema, spec.Definitions["brand"])
	assert.Equal(t, *schema.AllOf[1].Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for AnyOfBoth, in the returned schema [expand anyOf]
	schema = spec.Definitions["anyofBoth"]
	s, err = expandSchema(schema, []string{"#/definitions/anyofBoth"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AnyOf[0].Items.Schema.Ref.String())
	assert.Empty(t, schema.AnyOf[1].Items.Schema.Ref.String())
	assert.Equal(t, *schema.AnyOf[0].Items.Schema, spec.Definitions["brand"])
	assert.Equal(t, *schema.AnyOf[1].Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for OneOfBoth, in the returned schema [expand oneOf]
	schema = spec.Definitions["oneofBoth"]
	s, err = expandSchema(schema, []string{"#/definitions/oneofBoth"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.OneOf[0].Items.Schema.Ref.String())
	assert.Empty(t, schema.OneOf[1].Items.Schema.Ref.String())
	assert.Equal(t, *schema.OneOf[0].Items.Schema, spec.Definitions["brand"])
	assert.Equal(t, *schema.OneOf[1].Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for NotSomething, in the returned schema [expand not]
	schema = spec.Definitions["notSomething"]
	s, err = expandSchema(schema, []string{"#/definitions/notSomething"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Not.Items.Schema.Ref.String())
	assert.Equal(t, *schema.Not.Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for WithAdditional, in the returned schema [expand additionalProperties]
	schema = spec.Definitions["withAdditional"]
	s, err = expandSchema(schema, []string{"#/definitions/withAdditional"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AdditionalProperties.Schema.Items.Schema.Ref.String())
	assert.Equal(t, *schema.AdditionalProperties.Schema.Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for WithAdditionalItems, in the returned schema [expand additionalItems]
	schema = spec.Definitions["withAdditionalItems"]
	s, err = expandSchema(schema, []string{"#/definitions/withAdditionalItems"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AdditionalItems.Schema.Items.Schema.Ref.String())
	assert.Equal(t, *schema.AdditionalItems.Schema.Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for WithPattern, in the returned schema [expand PatternProperties]
	schema = spec.Definitions["withPattern"]
	s, err = expandSchema(schema, []string{"#/definitions/withPattern"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	prop := schema.PatternProperties["^x-ab"]
	assert.Empty(t, prop.Items.Schema.Ref.String())
	assert.Equal(t, *prop.Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for Deps, in the returned schema [expand dependencies]
	schema = spec.Definitions["deps"]
	s, err = expandSchema(schema, []string{"#/definitions/deps"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	prop2 := schema.Dependencies["something"]
	assert.Empty(t, prop2.Schema.Items.Schema.Ref.String())
	assert.Equal(t, *prop2.Schema.Items.Schema, spec.Definitions["tag"])

	// verify expanded schema for Defined, in the returned schema [expand nested definitions]
	schema = spec.Definitions["defined"]
	s, err = expandSchema(schema, []string{"#/definitions/defined"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	prop = schema.Definitions["something"]
	assert.Empty(t, prop.Items.Schema.Ref.String())
	assert.Equal(t, *prop.Items.Schema, spec.Definitions["tag"])
}

func TestExpand_InternalSchemas1(t *testing.T) {
	basePath := normalizeBase(filepath.Join("fixtures", "expansion", "schemas1.json"))

	carsDoc, err := jsonDoc(basePath)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(carsDoc, spec))

	resolver := defaultSchemaLoader(spec, nil, nil, nil)

	schema := spec.Definitions["car"]
	oldBrand := schema.Properties["brand"]
	assert.NotEmpty(t, oldBrand.Ref.String())
	assert.NotEqual(t, spec.Definitions["brand"], oldBrand)

	s, err := expandSchema(schema, []string{"#/definitions/car"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s

	newBrand := schema.Properties["brand"]
	assert.Empty(t, newBrand.Ref.String())
	assert.Equal(t, spec.Definitions["brand"], newBrand)

	schema = spec.Definitions["truck"]
	assert.NotEmpty(t, schema.Ref.String())

	s, err = expandSchema(schema, []string{"#/definitions/truck"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Ref.String())
	assert.Equal(t, spec.Definitions["car"], schema)

	sch := new(Schema)
	_, err = expandSchema(*sch, []string{""}, resolver, basePath)
	require.NoError(t, err)

	schema = spec.Definitions["batch"]
	s, err = expandSchema(schema, []string{"#/definitions/batch"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Items.Schema.Ref.String())
	assert.Equal(t, *schema.Items.Schema, spec.Definitions["brand"])

	schema = spec.Definitions["batch2"]
	s, err = expandSchema(schema, []string{"#/definitions/batch2"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Items.Schemas[0].Ref.String())
	assert.Empty(t, schema.Items.Schemas[1].Ref.String())
	assert.Equal(t, schema.Items.Schemas[0], spec.Definitions["brand"])
	assert.Equal(t, schema.Items.Schemas[1], spec.Definitions["tag"])

	schema = spec.Definitions["allofBoth"]
	s, err = expandSchema(schema, []string{"#/definitions/allofBoth"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AllOf[0].Ref.String())
	assert.Empty(t, schema.AllOf[1].Ref.String())
	assert.Equal(t, schema.AllOf[0], spec.Definitions["brand"])
	assert.Equal(t, schema.AllOf[1], spec.Definitions["tag"])

	schema = spec.Definitions["anyofBoth"]
	s, err = expandSchema(schema, []string{"#/definitions/anyofBoth"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AnyOf[0].Ref.String())
	assert.Empty(t, schema.AnyOf[1].Ref.String())
	assert.Equal(t, schema.AnyOf[0], spec.Definitions["brand"])
	assert.Equal(t, schema.AnyOf[1], spec.Definitions["tag"])

	schema = spec.Definitions["oneofBoth"]
	s, err = expandSchema(schema, []string{"#/definitions/oneofBoth"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.OneOf[0].Ref.String())
	assert.Empty(t, schema.OneOf[1].Ref.String())
	assert.Equal(t, schema.OneOf[0], spec.Definitions["brand"])
	assert.Equal(t, schema.OneOf[1], spec.Definitions["tag"])

	schema = spec.Definitions["notSomething"]
	s, err = expandSchema(schema, []string{"#/definitions/notSomething"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.Not.Ref.String())
	assert.Equal(t, *schema.Not, spec.Definitions["tag"])

	schema = spec.Definitions["withAdditional"]
	s, err = expandSchema(schema, []string{"#/definitions/withAdditional"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AdditionalProperties.Schema.Ref.String())
	assert.Equal(t, *schema.AdditionalProperties.Schema, spec.Definitions["tag"])

	schema = spec.Definitions["withAdditionalItems"]
	s, err = expandSchema(schema, []string{"#/definitions/withAdditionalItems"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	assert.Empty(t, schema.AdditionalItems.Schema.Ref.String())
	assert.Equal(t, *schema.AdditionalItems.Schema, spec.Definitions["tag"])

	schema = spec.Definitions["withPattern"]
	s, err = expandSchema(schema, []string{"#/definitions/withPattern"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	prop := schema.PatternProperties["^x-ab"]
	assert.Empty(t, prop.Ref.String())
	assert.Equal(t, prop, spec.Definitions["tag"])

	schema = spec.Definitions["deps"]
	s, err = expandSchema(schema, []string{"#/definitions/deps"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	prop2 := schema.Dependencies["something"]
	assert.Empty(t, prop2.Schema.Ref.String())
	assert.Equal(t, *prop2.Schema, spec.Definitions["tag"])

	schema = spec.Definitions["defined"]
	s, err = expandSchema(schema, []string{"#/definitions/defined"}, resolver, basePath)
	require.NoError(t, err)
	require.NotNil(t, s)

	schema = *s
	prop = schema.Definitions["something"]
	assert.Empty(t, prop.Ref.String())
	assert.Equal(t, prop, spec.Definitions["tag"])
}

func TestExpand_RelativeBaseURI(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.Dir("fixtures/remote")))
	defer server.Close()

	spec := new(Swagger)

	require.NoError(t, ExpandSpec(spec, nil))

	// this a spec on local fs...
	specDoc, err := jsonDoc("fixtures/remote/all-the-things.json")
	require.NoError(t, err)

	spec = new(Swagger)
	require.NoError(t, json.Unmarshal(specDoc, spec))

	pet := spec.Definitions["pet"]
	errorModel := spec.Definitions["errorModel"]
	petResponse := spec.Responses["petResponse"]
	petResponse.Schema = &pet
	stringResponse := spec.Responses["stringResponse"]
	tagParam := spec.Parameters["tag"]
	idParam := spec.Parameters["idParam"]
	anotherPet := spec.Responses["anotherPet"]

	// ... with some $ref with http scheme
	anotherPet.Ref = MustCreateRef(server.URL + "/" + anotherPet.Ref.String())

	opts := &ExpandOptions{
		RelativeBase: server.URL + "/all-the-things.json",
	}

	require.NoError(t, ExpandResponse(&anotherPet, opts.RelativeBase))

	spec.Responses["anotherPet"] = anotherPet

	circularA := spec.Responses["circularA"]
	circularA.Ref = MustCreateRef(server.URL + "/" + circularA.Ref.String())

	require.NoError(t, ExpandResponse(&circularA, opts.RelativeBase))

	// circularA is self-referencing: results in an empty response: this is okay and expected
	// for the expand use case. The flatten use case should however be expected to fail on this.
	assert.Empty(t, circularA.Description)
	assert.Empty(t, circularA.Ref)

	spec.Responses["circularA"] = circularA

	// expand again, no issue should arise
	require.NoError(t, ExpandSpec(spec, opts))

	// backRef navigates back to the root document (relative $ref)
	backRef := spec.Responses["backRef"]
	require.NoError(t, ExpandResponse(&backRef, opts.RelativeBase))
	assert.Equal(t, "pet response", backRef.Description)
	assert.NotEmpty(t, backRef.Schema)
	assert.Empty(t, backRef.Ref)

	assert.Equal(t, tagParam, spec.Parameters["query"])

	assert.Equal(t, petResponse, spec.Responses["petResponse"])
	assert.Equal(t, petResponse, spec.Responses["anotherPet"])
	assert.Equal(t, petResponse, spec.Paths.Paths["/pets"].Post.Responses.StatusCodeResponses[200])
	assert.Equal(t, petResponse, spec.Paths.Paths["/"].Get.Responses.StatusCodeResponses[200])

	assert.Equal(t, pet, *spec.Paths.Paths["/pets"].Get.Responses.StatusCodeResponses[200].Schema.Items.Schema)
	assert.Equal(t, pet, *spec.Responses["petResponse"].Schema)
	assert.Equal(t, pet, spec.Definitions["petInput"].AllOf[0])

	assert.Equal(t, spec.Definitions["petInput"], *spec.Paths.Paths["/pets"].Post.Parameters[0].Schema)

	assert.Equal(t, stringResponse, *spec.Paths.Paths["/"].Get.Responses.Default)

	assert.Equal(t, errorModel, *spec.Paths.Paths["/pets"].Get.Responses.Default.Schema)
	assert.Equal(t, errorModel, *spec.Paths.Paths["/pets"].Post.Responses.Default.Schema)

	pi := spec.Paths.Paths["/pets/{id}"]
	assert.Equal(t, idParam, pi.Get.Parameters[0])
	assert.Equal(t, petResponse, pi.Get.Responses.StatusCodeResponses[200])
	assert.Equal(t, idParam, pi.Delete.Parameters[0])

	assert.Equal(t, errorModel, *pi.Get.Responses.Default.Schema)
	assert.Equal(t, errorModel, *pi.Delete.Responses.Default.Schema)
}

func resolutionContextServer() *httptest.Server {
	var servedAt string
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/resolution.json" {

			b, _ := ioutil.ReadFile(filepath.Join(specs, "resolution.json"))
			var ctnt map[string]interface{}
			_ = json.Unmarshal(b, &ctnt)
			ctnt["id"] = servedAt

			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(200)
			bb, _ := json.Marshal(ctnt)
			_, _ = rw.Write(bb)
			return
		}
		if req.URL.Path == "/resolution2.json" {
			b, _ := ioutil.ReadFile(filepath.Join(specs, "resolution2.json"))
			var ctnt map[string]interface{}
			_ = json.Unmarshal(b, &ctnt)
			ctnt["id"] = servedAt

			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(200)
			bb, _ := json.Marshal(ctnt)
			_, _ = rw.Write(bb)
			return
		}

		if req.URL.Path == "/boolProp.json" {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(200)
			b, _ := json.Marshal(map[string]interface{}{
				"type": "boolean",
			})
			_, _ = rw.Write(b)
			return
		}

		if req.URL.Path == "/deeper/stringProp.json" {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(200)
			b, _ := json.Marshal(map[string]interface{}{
				"type": "string",
			})
			_, _ = rw.Write(b)
			return
		}

		if req.URL.Path == "/deeper/arrayProp.json" {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(200)
			b, _ := json.Marshal(map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "file",
				},
			})
			_, _ = rw.Write(b)
			return
		}

		rw.WriteHeader(http.StatusNotFound)
	}))
	servedAt = server.URL
	return server
}

func TestExpand_RemoteRefWithResolutionContext(t *testing.T) {
	server := resolutionContextServer()
	defer server.Close()

	tgt := RefSchema(server.URL + "/resolution.json#/definitions/bool")
	require.NoError(t, ExpandSchema(tgt, nil, nil))

	assert.Equal(t, StringOrArray([]string{"boolean"}), tgt.Type)
	assert.Empty(t, tgt.Ref)
}

func TestExpandRemoteRef_WithNestedResolutionContext(t *testing.T) {
	server := resolutionContextServer()
	defer server.Close()

	tgt := RefSchema(server.URL + "/resolution.json#/items")
	require.NoError(t, ExpandSchema(tgt, nil, nil))

	require.Empty(t, tgt.Ref)
	require.NotNil(t, tgt.Items)
	require.NotNil(t, tgt.Schema)
	assert.Equal(t, "deeper/", tgt.ID) // schema id is preserved

	assert.Equal(t, StringOrArray([]string{"string"}), tgt.Items.Schema.Type)
	assert.Empty(t, tgt.Items.Schema.Ref)
}

/*
   This next test will have to wait until we do full $ID analysis for every subschema on every file that is referenced
// For now, TestExpandRemoteRef_WithNestedResolutionContext replaces this next test
func TestExpandRemoteRef_WithNestedResolutionContext_WithParentID(t *testing.T) {
 	server := resolutionContextServer()
 	defer server.Close()

	tgt := RefSchema(server.URL + "/resolution.json#/items/items")
	require.NoError(t, ExpandSchema(tgt, nil, nil))
	t.Logf("%s", asJSON(t, tgt))

	assert.Equal(t, StringOrArray([]string{"string"}), tgt.Type)
}
*/

func TestExpand_RemoteRefWithNestedResolutionContextWithFragment(t *testing.T) {
	server := resolutionContextServer()
	defer server.Close()

	tgt := RefSchema(server.URL + "/resolution2.json#/items")
	require.NoError(t, ExpandSchema(tgt, nil, nil))

	require.Empty(t, tgt.Ref)
	require.NotNil(t, tgt.Items)
	require.NotNil(t, tgt.Schema)
	assert.Equal(t, "deeper/", tgt.ID) // schema id is preserved

	assert.Equal(t, StringOrArray([]string{"file"}), tgt.Items.Schema.Type)
	assert.Empty(t, tgt.Items.Schema.Ref)
}

func TestExpand_TransitiveRefs(t *testing.T) {
	basePath := filepath.Join(specs, "todos.json")

	rawSpec, err := ioutil.ReadFile(basePath)
	require.NoError(t, err)

	var spec *Swagger
	require.NoError(t, json.Unmarshal(rawSpec, &spec))

	opts := &ExpandOptions{
		RelativeBase: basePath,
	}

	require.NoError(t, ExpandSpec(spec, opts))

	assert.Equal(t, "todos.stoplight.io", spec.Host) // i.e. not empty
	jazon := asJSON(t, spec)

	// verify that the spec has been fully expanded
	assertNoRef(t, jazon)
}

func TestExpand_SchemaWithRoot(t *testing.T) {
	root := new(Swagger)
	require.NoError(t, json.Unmarshal(PetStoreJSONMessage, root))

	// 1. remove ID from root definition
	origPet := root.Definitions["Pet"]
	newPet := origPet
	newPet.ID = ""
	root.Definitions["Pet"] = newPet
	expandRootWithID(t, root, withoutSchemaID)

	// 2. put back ID in Pet definition
	// nested $ref should fail
	root.Definitions["Pet"] = origPet
	expandRootWithID(t, root, withSchemaID)
}

func expandRootWithID(t testing.TB, root *Swagger, testcase string) {
	t.Logf("case: expanding $ref to schema without ID, with nested $ref with %s ID", testcase)
	sch := RefSchema("#/definitions/newPet")
	err := ExpandSchema(sch, root, nil)

	if testcase == withSchemaID {
		require.Errorf(t, err, "expected %s NOT to expand properly because of the ID in the parent schema", sch.Ref.String())
	} else {
		require.NoErrorf(t, err, "expected %s to expand properly", sch.Ref.String())

		// verify that the spec has been fully expanded
		jazon := asJSON(t, sch)
		assertNoRef(t, jazon)
	}

	t.Log("case: expanding $ref to schema without nested $ref")
	sch = RefSchema("#/definitions/Category")
	require.NoErrorf(t, ExpandSchema(sch, root, nil), "expected %s to expand properly", sch.Ref.String())

	t.Logf("case: expanding $ref to schema with %s ID and nested $ref", testcase)
	sch = RefSchema("#/definitions/Pet")
	err = ExpandSchema(sch, root, nil)

	if testcase == withSchemaID {
		require.Errorf(t, err, "expected %s NOT to expand properly because of the ID in the parent schema", sch.Ref.String())
	} else {
		require.NoErrorf(t, err, "expected %s to expand properly", sch.Ref.String())

		// verify that the spec has been fully expanded
		jazon := asJSON(t, sch)
		assertNoRef(t, jazon)
	}
}

func TestExpand_PathItem(t *testing.T) {
	jazon, _ := expandThisOrDieTrying(t, pathItemsFixture)
	assert.JSONEq(t, `{
         "swagger": "2.0",
         "info": {
          "title": "PathItems refs",
          "version": "1.0"
         },
         "paths": {
          "/todos": {
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
          }
         }
			 }`, jazon)
}

func TestExpand_ExtraItems(t *testing.T) {
	jazon, _ := expandThisOrDieTrying(t, extraRefFixture)
	assert.JSONEq(t, `{
         "schemes": [
          "http"
         ],
         "swagger": "2.0",
         "info": {
          "title": "Supported, but non Swagger 20 compliant $ref constructs",
          "version": "2.1.0"
         },
         "host": "item.com",
         "basePath": "/extraRefs",
         "paths": {
          "/employees": {
           "get": {
            "summary": "List Employee Types",
            "operationId": "LIST-Employees",
            "parameters": [
             {
							"description": "unsupported $ref in simple param",
              "type": "array",
              "items": {
               "$ref": "#/definitions/arrayType"
              },
              "name": "myQueryParam",
              "in": "query"
             }
            ],
            "responses": {
             "200": {
							"description": "unsupported $ref in header",
              "schema": {
               "type": "string"
              },
              "headers": {
               "X-header": {
                  "type": "array",
                  "items": {
                    "$ref": "#/definitions/headerType"
                  }
							  }
              }
             }
            }
           }
          }
         },
         "definitions": {
          "arrayType": {
           "type": "integer",
           "format": "int32"
          },
          "headerType": {
           "type": "string",
           "format": "uuid"
          }
         }
			 }`, jazon)
}

// PetStore20 json doc for swagger 2.0 pet store
const PetStore20 = `{
  "swagger": "2.0",
  "info": {
    "version": "1.0.0",
    "title": "Swagger Petstore",
    "contact": {
      "name": "Wordnik API Team",
      "url": "http://developer.wordnik.com"
    },
    "license": {
      "name": "Creative Commons 4.0 International",
      "url": "http://creativecommons.org/licenses/by/4.0/"
    }
  },
  "host": "petstore.swagger.wordnik.com",
  "basePath": "/api",
  "schemes": [
    "http"
  ],
  "paths": {
    "/pets": {
      "get": {
        "security": [
          {
            "basic": []
          }
        ],
        "tags": [ "Pet Operations" ],
        "operationId": "getAllPets",
        "parameters": [
          {
            "name": "status",
            "in": "query",
            "description": "The status to filter by",
            "type": "string"
          },
          {
            "name": "limit",
            "in": "query",
            "description": "The maximum number of results to return",
            "type": "integer",
						"format": "int64"
          }
        ],
        "summary": "Finds all pets in the system",
        "responses": {
          "200": {
            "description": "Pet response",
            "schema": {
              "type": "array",
              "items": {
                "$ref": "#/definitions/Pet"
              }
            }
          },
          "default": {
            "description": "Unexpected error",
            "schema": {
              "$ref": "#/definitions/Error"
            }
          }
        }
      },
      "post": {
        "security": [
          {
            "basic": []
          }
        ],
        "tags": [ "Pet Operations" ],
        "operationId": "createPet",
        "summary": "Creates a new pet",
        "consumes": ["application/x-yaml"],
        "produces": ["application/x-yaml"],
        "parameters": [
          {
            "name": "pet",
            "in": "body",
            "description": "The Pet to create",
            "required": true,
            "schema": {
              "$ref": "#/definitions/newPet"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Created Pet response",
            "schema": {
              "$ref": "#/definitions/Pet"
            }
          },
          "default": {
            "description": "Unexpected error",
            "schema": {
              "$ref": "#/definitions/Error"
            }
          }
        }
      }
    },
    "/pets/{id}": {
      "delete": {
        "security": [
          {
            "apiKey": []
          }
        ],
        "description": "Deletes the Pet by id",
        "operationId": "deletePet",
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "description": "ID of pet to delete",
            "required": true,
            "type": "integer",
            "format": "int64"
          }
        ],
        "responses": {
          "204": {
            "description": "pet deleted"
          },
          "default": {
            "description": "unexpected error",
            "schema": {
              "$ref": "#/definitions/Error"
            }
          }
        }
      },
      "get": {
        "tags": [ "Pet Operations" ],
        "operationId": "getPetById",
        "summary": "Finds the pet by id",
        "responses": {
          "200": {
            "description": "Pet response",
            "schema": {
              "$ref": "#/definitions/Pet"
            }
          },
          "default": {
            "description": "Unexpected error",
            "schema": {
              "$ref": "#/definitions/Error"
            }
          }
        }
      },
      "parameters": [
        {
          "name": "id",
          "in": "path",
          "description": "ID of pet",
          "required": true,
          "type": "integer",
          "format": "int64"
        }
      ]
    }
  },
  "definitions": {
    "Category": {
      "id": "Category",
      "properties": {
        "id": {
          "format": "int64",
          "type": "integer"
        },
        "name": {
          "type": "string"
        }
      }
    },
    "Pet": {
      "id": "Pet",
      "properties": {
        "category": {
          "$ref": "#/definitions/Category"
        },
        "id": {
          "description": "unique identifier for the pet",
          "format": "int64",
          "maximum": 100.0,
          "minimum": 0.0,
          "type": "integer"
        },
        "name": {
          "type": "string"
        },
        "photoUrls": {
          "items": {
            "type": "string"
          },
          "type": "array"
        },
        "status": {
          "description": "pet status in the store",
          "enum": [
            "available",
            "pending",
            "sold"
          ],
          "type": "string"
        },
        "tags": {
          "items": {
            "$ref": "#/definitions/Tag"
          },
          "type": "array"
        }
      },
      "required": [
        "id",
        "name"
      ]
    },
    "newPet": {
      "anyOf": [
        {
          "$ref": "#/definitions/Pet"
        },
        {
          "required": [
            "name"
          ]
        }
      ]
    },
    "Tag": {
      "id": "Tag",
      "properties": {
        "id": {
          "format": "int64",
          "type": "integer"
        },
        "name": {
          "type": "string"
        }
      }
    },
    "Error": {
      "required": [
        "code",
        "message"
      ],
      "properties": {
        "code": {
          "type": "integer",
          "format": "int32"
        },
        "message": {
          "type": "string"
        }
      }
    }
  },
  "consumes": [
    "application/json",
    "application/xml"
  ],
  "produces": [
    "application/json",
    "application/xml",
    "text/plain",
    "text/html"
  ],
  "securityDefinitions": {
    "basic": {
      "type": "basic"
    },
    "apiKey": {
      "type": "apiKey",
      "in": "header",
      "name": "X-API-KEY"
    }
  }
}
`
