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

package spec_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-openapi/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test unitary fixture for dev and bug fixing
func TestSpec_Issue1429(t *testing.T) {
	path := filepath.Join("fixtures", "bugs", "1429", "swagger.yaml")

	// load and full expand
	sp := loadOrFail(t, path)
	err := spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader})
	require.NoError(t, err)

	// assert well expanded
	require.Truef(t, (sp.Paths != nil && sp.Paths.Paths != nil), "expected paths to be available in fixture")

	assertPaths1429(t, sp)

	for _, def := range sp.Definitions {
		assert.Empty(t, def.Ref)
	}

	// reload and SkipSchemas: true
	sp = loadOrFail(t, path)
	err = spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: true, PathLoader: testLoader})
	require.NoError(t, err)

	// assert well resolved
	require.Truef(t, (sp.Paths != nil && sp.Paths.Paths != nil), "expected paths to be available in fixture")

	assertPaths1429SkipSchema(t, sp)

	for _, def := range sp.Definitions {
		assert.Contains(t, def.Ref.String(), "responses.yaml#/definitions/")
	}
}

func assertPaths1429(t testing.TB, sp *spec.Swagger) {
	for _, pi := range sp.Paths.Paths {
		for _, param := range pi.Get.Parameters {
			require.NotNilf(t, param.Schema, "expected param schema not to be nil")
			// all param fixtures are body param with schema
			// all $ref expanded
			assert.Equal(t, "", param.Schema.Ref.String())
		}

		for code, response := range pi.Get.Responses.StatusCodeResponses {
			// all response fixtures are with StatusCodeResponses, but 200
			if code == 200 {
				assert.Nilf(t, response.Schema, "expected response schema to be nil")
				continue
			}
			require.NotNilf(t, response.Schema, "expected response schema not to be nil")
			assert.Equal(t, "", response.Schema.Ref.String())
		}
	}
}

func assertPaths1429SkipSchema(t testing.TB, sp *spec.Swagger) {
	for _, pi := range sp.Paths.Paths {
		for _, param := range pi.Get.Parameters {
			require.NotNilf(t, param.Schema, "expected param schema not to be nil")

			// all param fixtures are body param with schema
			switch param.Name {
			case "plainRequest":
				// this one is expanded
				assert.Equal(t, "", param.Schema.Ref.String())
				continue
			case "nestedBody":
				// this one is local
				assert.Truef(t, strings.HasPrefix(param.Schema.Ref.String(), "#/definitions/"),
					"expected rooted definitions $ref, got: %s", param.Schema.Ref.String())
				continue
			case "remoteRequest":
				assert.Contains(t, param.Schema.Ref.String(), "remote/remote.yaml#/")
				continue
			}
			assert.Contains(t, param.Schema.Ref.String(), "responses.yaml#/")

		}

		for code, response := range pi.Get.Responses.StatusCodeResponses {
			// all response fixtures are with StatusCodeResponses, but 200
			switch code {
			case 200:
				assert.Nilf(t, response.Schema, "expected response schema to be nil")
				continue
			case 204:
				assert.Contains(t, response.Schema.Ref.String(), "remote/remote.yaml#/")
				continue
			case 404:
				assert.Equal(t, "", response.Schema.Ref.String())
				continue
			}
			assert.Containsf(t, response.Schema.Ref.String(), "responses.yaml#/", "expected remote ref at resp. %d", code)
		}
	}
}

func TestSpec_MoreLocalExpansion(t *testing.T) {
	path := filepath.Join("fixtures", "local_expansion", "spec2.yaml")

	// load and full expand
	sp := loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader}))

	// asserts all $ref are expanded
	assert.NotContains(t, asJSON(t, sp), `"$ref"`)
}

func TestSpec_Issue69(t *testing.T) {
	// this checks expansion for the dapperbox spec (circular ref issues)

	path := filepath.Join("fixtures", "bugs", "69", "dapperbox.json")

	// expand with relative path
	// load and expand
	sp := loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader}))

	// asserts all $ref expanded
	jazon := asJSON(t, sp)

	// circular $ref are not expanded: however, they point to the expanded root document

	// assert all $ref match  "$ref": "#/definitions/something"
	assertRefInJSON(t, jazon, "#/definitions")

	// assert all $ref expand correctly against the spec
	assertRefExpand(t, jazon, "", sp)
}

func TestSpec_Issue1621(t *testing.T) {
	path := filepath.Join("fixtures", "bugs", "1621", "fixture-1621.yaml")

	// expand with relative path
	// load and expand
	sp := loadOrFail(t, path)

	err := spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader})
	require.NoError(t, err)

	// asserts all $ref expanded
	jazon := asJSON(t, sp)

	assertNoRef(t, jazon)
}

func TestSpec_Issue1614(t *testing.T) {
	path := filepath.Join("fixtures", "bugs", "1614", "gitea.json")

	// expand with relative path
	// load and expand
	sp := loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader}))

	// asserts all $ref expanded
	jazon := asJSON(t, sp)

	// assert all $ref match  "$ref": "#/definitions/something"
	assertRefInJSON(t, jazon, "#/definitions")

	// assert all $ref expand correctly against the spec
	assertRefExpand(t, jazon, "", sp)

	// now with option CircularRefAbsolute: circular $ref are not denormalized and are kept absolute.
	// This option is essentially for debugging purpose.
	sp = loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{
		RelativeBase:        path,
		SkipSchemas:         false,
		AbsoluteCircularRef: true,
		PathLoader:          testLoader,
	}))

	// asserts all $ref expanded
	jazon = asJSON(t, sp)

	// assert all $ref match  "$ref": "file://{file}#/definitions/something"
	assertRefInJSONRegexp(t, jazon, `file://.*/gitea.json#/definitions/`)

	// assert all $ref expand correctly against the spec
	assertRefExpand(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path})
}

func TestSpec_Issue2113(t *testing.T) {
	// this checks expansion with nested specs
	path := filepath.Join("fixtures", "bugs", "2113", "base.yaml")

	// load and expand
	sp := loadOrFail(t, path)
	err := spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader})
	require.NoError(t, err)

	// asserts all $ref expanded
	jazon := asJSON(t, sp)

	// assert all $ref match have been expanded
	assertNoRef(t, jazon)

	// now trying with SkipSchemas
	sp = loadOrFail(t, path)
	err = spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: true, PathLoader: testLoader})
	require.NoError(t, err)

	jazon = asJSON(t, sp)

	// assert all $ref match
	assertRefInJSONRegexp(t, jazon, `^(schemas/dummy/dummy.yaml)|(schemas/example/example.yaml)`)

	// assert all $ref resolve correctly against the spec
	assertRefResolve(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})

	// assert all $ref expand correctly against the spec
	assertRefExpand(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})
}

func TestSpec_Issue2113_External(t *testing.T) {
	// Exercises the SkipSchema mode (used by spec flattening in go-openapi/analysis).
	// Provides more ground for testing with schemas nested in $refs

	// this checks expansion with nested specs
	path := filepath.Join("fixtures", "skipschema", "external_definitions_valid.yml")

	// load and expand, skipping schema expansion
	sp := loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: true, PathLoader: testLoader}))

	// asserts all $ref are expanded as expected
	jazon := asJSON(t, sp)

	assertRefInJSONRegexp(t, jazon, `^(external/definitions.yml#/definitions)|(external/errors.yml#/error)|(external/nestedParams.yml#/bodyParam)`)

	// assert all $ref resolve correctly against the spec
	assertRefResolve(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})

	// assert all $ref in jazon expand correctly against the spec
	assertRefExpand(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})

	// expansion can be iterated again, including schemas
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader}))

	jazon = asJSON(t, sp)
	assertNoRef(t, jazon)

	// load and expand everything
	sp = loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader}))

	jazon = asJSON(t, sp)
	assertNoRef(t, jazon)
}

func TestSpec_Issue2113_SkipSchema(t *testing.T) {
	// Exercises the SkipSchema mode from spec flattening in go-openapi/analysis
	// Provides more ground for testing with schemas nested in $refs

	// this checks expansion with nested specs
	path := filepath.Join("fixtures", "flatten", "flatten.yml")

	// load and expand, skipping schema expansion
	sp := loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: true, PathLoader: testLoader}))

	jazon := asJSON(t, sp)

	// asserts all $ref are expanded as expected
	assertRefInJSONRegexp(t, jazon, `^(external/definitions.yml#/definitions)|(#/definitions/namedAgain)|(external/errors.yml#/error)`)

	// assert all $ref resolve correctly against the spec
	assertRefResolve(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})

	assertRefExpand(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})

	// load and expand, including schemas
	sp = loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader}))

	jazon = asJSON(t, sp)
	assertNoRef(t, jazon)
}

func TestSpec_PointersLoop(t *testing.T) {
	// this a spec that cannot be flattened (self-referencing pointer).
	// however, it should be expanded without errors

	// this checks expansion with nested specs
	path := filepath.Join("fixtures", "more_circulars", "pointers", "fixture-pointers-loop.yaml")

	// load and expand, skipping schema expansion
	sp := loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: true, PathLoader: testLoader}))

	jazon := asJSON(t, sp)
	assertRefExpand(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})

	sp = loadOrFail(t, path)
	require.NoError(t, spec.ExpandSpec(sp, &spec.ExpandOptions{RelativeBase: path, SkipSchemas: false, PathLoader: testLoader}))

	// cannot guarantee which ref will be kept, but only one remains: expand reduces all $ref down
	// to the last self-referencing one (the one picked changes from one run to another, depending
	// on where during the walk the cycle is detected).
	jazon = asJSON(t, sp)

	m := rex.FindAllStringSubmatch(jazon, -1)
	require.NotEmpty(t, m)

	refs := make(map[string]struct{}, 5)
	for _, matched := range m {
		subMatch := matched[1]
		refs[subMatch] = struct{}{}
	}
	require.Len(t, refs, 1)

	assertRefExpand(t, jazon, "", sp, &spec.ExpandOptions{RelativeBase: path, PathLoader: testLoader})
}

func TestSpec_Issue102(t *testing.T) {
	// go-openapi/validate/issues#102
	path := filepath.Join("fixtures", "bugs", "102", "fixture-102.json")
	sp := loadOrFail(t, path)

	require.NoError(t, spec.ExpandSpec(sp, nil))

	jazon := asJSON(t, sp)
	assertRefInJSONRegexp(t, jazon, `^#/definitions/Error$`)

	sp = loadOrFail(t, path)
	sch := spec.RefSchema("#/definitions/Error")
	require.NoError(t, spec.ExpandSchema(sch, sp, nil))

	jazon = asJSON(t, sch)
	assertRefInJSONRegexp(t, jazon, "^#/definitions/Error$")

	sp = loadOrFail(t, path)
	sch = spec.RefSchema("#/definitions/Error")
	resp := spec.NewResponse().WithDescription("ok").WithSchema(sch)
	require.NoError(t, spec.ExpandResponseWithRoot(resp, sp, nil))

	jazon = asJSON(t, resp)
	assertRefInJSONRegexp(t, jazon, "^#/definitions/Error$")

	sp = loadOrFail(t, path)
	sch = spec.RefSchema("#/definitions/Error")
	param := spec.BodyParam("error", sch)
	require.NoError(t, spec.ExpandParameterWithRoot(param, sp, nil))

	jazon = asJSON(t, resp)
	assertRefInJSONRegexp(t, jazon, "^#/definitions/Error$")
}
