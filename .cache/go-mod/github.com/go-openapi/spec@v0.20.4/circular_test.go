package spec

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandCircular_Issue3(t *testing.T) {
	jazon, root := expandThisOrDieTrying(t, "fixtures/expansion/overflow.json")
	require.NotEmpty(t, jazon)

	// circular $ref point to the expanded root document
	assertRefInJSON(t, jazon, "#/definitions")

	// verify that all $ref can resolved against the new root schema
	assertRefResolve(t, jazon, "", root)

	// verify that all $ref can be expanded in the new root schema
	assertRefExpand(t, jazon, "", root)
}

func TestExpandCircular_RefExpansion(t *testing.T) {
	basePath := filepath.Join("fixtures", "expansion", "circularRefs.json")

	carsDoc, err := jsonDoc(basePath)
	require.NoError(t, err)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(carsDoc, spec))

	resolver := defaultSchemaLoader(spec, &ExpandOptions{RelativeBase: basePath}, nil, nil)

	schema := spec.Definitions["car"]

	_, err = expandSchema(schema, []string{"#/definitions/car"}, resolver, normalizeBase(basePath))
	require.NoError(t, err)

	jazon := asJSON(t, schema)

	// circular $ref point to the expanded root document
	// there are only 2 types with circular definitions
	assertRefInJSONRegexp(t, jazon, "#/definitions/(car|category)")

	// verify that all $ref can resolved against the new root schema
	assertRefResolve(t, jazon, "", spec)

	// verify that all $ref can be expanded in the new root schema
	assertRefExpand(t, jazon, "", spec)
}

func TestExpandCircular_Spec2Expansion(t *testing.T) {
	// TODO: assert repeatable results (see commented section below)

	fixturePath := filepath.Join("fixtures", "expansion", "circular-minimal.json")
	jazon, root := expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)

	// circular $ref are not always the same, but they sure are one of the nodes
	assertRefInJSONRegexp(t, jazon, `#/definitions/node\d+`)

	// circular $ref always resolve against the root
	assertRefResolve(t, jazon, "", root)

	// assert stripped $ref in result
	assert.NotContainsf(t, jazon, "circular-minimal.json#/",
		"expected %s to be expanded with stripped circular $ref", fixturePath)

	fixturePath = filepath.Join("fixtures", "expansion", "circularSpec2.json")
	jazon, root = expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)

	// circular $ref resolved against the expanded root document
	assertRefInJSON(t, jazon, `#/definitions/`)

	// circular $ref always resolve against the root
	assertRefResolve(t, jazon, "", root)

	// circular $ref can always be further expanded against the root
	assertRefExpand(t, jazon, "", root)

	assert.NotContainsf(t, jazon, "circularSpec.json#/",
		"expected %s to be expanded with stripped circular $ref", fixturePath)

	/*

		At the moment, the result of expanding circular references is not stable,
		when several cycles have intersections:
		the spec structure is randomly walked through and mutating as expansion is carried out.
		detected cycles in $ref are not necessarily the shortest matches.

		This may result in different, functionally correct expanded specs (e.g. with same validations)

			for i := 0; i < 1; i++ {
				bbb := expandThisOrDieTrying(t, fixturePath)
				t.Log(bbb)
				if !assert.JSONEqf(t, jazon, bbb, "on iteration %d, we should have stable expanded spec", i) {
					t.FailNow()
					return
				}
			}
	*/
}

func TestExpandCircular_MoreCircular(t *testing.T) {
	// Additional testcase for circular $ref (from go-openapi/validate):
	// - $ref with file = current file
	// - circular is located in remote file
	//
	// There are 4 variants to run:
	// - with/without $ref with local file (so its not really remote)
	// - with circular in a schema in  #/responses
	// - with circular in a schema in  #/parameters

	fixturePath := filepath.Join("fixtures", "more_circulars", "spec.json")
	jazon, root := expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)
	assertRefInJSON(t, jazon, "item.json#/item")
	assertRefResolve(t, jazon, "", root, &ExpandOptions{RelativeBase: fixturePath})

	fixturePath = filepath.Join("fixtures", "more_circulars", "spec2.json")
	jazon, root = expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)
	assertRefInJSON(t, jazon, "item2.json#/item")
	assertRefResolve(t, jazon, "", root, &ExpandOptions{RelativeBase: fixturePath})

	fixturePath = filepath.Join("fixtures", "more_circulars", "spec3.json")
	jazon, root = expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)
	assertRefInJSON(t, jazon, "item.json#/item")
	assertRefResolve(t, jazon, "", root, &ExpandOptions{RelativeBase: fixturePath})

	fixturePath = filepath.Join("fixtures", "more_circulars", "spec4.json")
	jazon, root = expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)
	assertRefInJSON(t, jazon, "item4.json#/item")
	assertRefResolve(t, jazon, "", root, &ExpandOptions{RelativeBase: fixturePath})
}

func TestExpandCircular_Issue957(t *testing.T) {
	fixturePath := filepath.Join("fixtures", "bugs", "957", "fixture-957.json")
	jazon, root := expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)

	require.NotContainsf(t, jazon, "fixture-957.json#/",
		"expected %s to be expanded with stripped circular $ref", fixturePath)

	assertRefInJSON(t, jazon, "#/definitions/")

	assertRefResolve(t, jazon, "", root)

	assertRefExpand(t, jazon, "", root)
}

func TestExpandCircular_Bitbucket(t *testing.T) {
	// Additional testcase for circular $ref (from bitbucket api)

	fixturePath := filepath.Join("fixtures", "more_circulars", "bitbucket.json")
	jazon, root := expandThisOrDieTrying(t, fixturePath)
	require.NotEmpty(t, jazon)

	assertRefInJSON(t, jazon, "#/definitions/")

	assertRefResolve(t, jazon, "", root)

	assertRefExpand(t, jazon, "", root)
}

func TestExpandCircular_ResponseWithRoot(t *testing.T) {
	rootDoc := new(Swagger)
	b, err := ioutil.ReadFile(filepath.Join("fixtures", "more_circulars", "resp.json"))
	require.NoError(t, err)

	require.NoError(t, json.Unmarshal(b, rootDoc))

	path := rootDoc.Paths.Paths["/api/v1/getx"]
	resp := path.Post.Responses.StatusCodeResponses[200]

	thisCache := cacheOrDefault(nil)

	// during the first response expand, refs are getting expanded,
	// so the following expands cannot properly resolve them w/o the document.
	// this happens in validator.Validate() when different validators try to expand the same mutable response.
	require.NoError(t, ExpandResponseWithRoot(&resp, rootDoc, thisCache))

	jazon := asJSON(t, resp)
	assertRefInJSON(t, jazon, "#/definitions/MyObj")

	// do it again
	require.NoError(t, ExpandResponseWithRoot(&resp, rootDoc, thisCache))
	jazon = asJSON(t, resp)
	assertRefInJSON(t, jazon, "#/definitions/MyObj")
}

func TestExpandCircular_Issue415(t *testing.T) {
	jazon, root := expandThisOrDieTrying(t, filepath.Join("fixtures", "expansion", "clickmeter.json"))
	require.NotEmpty(t, jazon)

	assertRefInJSON(t, jazon, "#/definitions/")
	assertRefResolve(t, jazon, "", root)
	assertRefExpand(t, jazon, "", root)
}

func TestExpandCircular_SpecExpansion(t *testing.T) {
	jazon, root := expandThisOrDieTrying(t, filepath.Join("fixtures", "expansion", "circularSpec.json"))
	require.NotEmpty(t, jazon)

	assertRefInJSON(t, jazon, "#/definitions/Book")
	assertRefResolve(t, jazon, "", root)
	assertRefExpand(t, jazon, "", root)
}

func TestExpandCircular_RemoteCircularID(t *testing.T) {
	go func() {
		err := http.ListenAndServe("localhost:1234", http.FileServer(http.Dir("fixtures/more_circulars/remote")))
		if err != nil {
			panic(err.Error())
		}
	}()
	time.Sleep(100 * time.Millisecond)

	// from json-schema test suite testcase for remote with circular ID
	fixturePath := "http://localhost:1234/tree"
	jazon, root := expandThisSchemaOrDieTrying(t, fixturePath)
	assertRefResolve(t, jazon, "", root, &ExpandOptions{RelativeBase: fixturePath})
	assertRefExpand(t, jazon, "", root, &ExpandOptions{RelativeBase: fixturePath})

	assert.NoError(t, ExpandSchemaWithBasePath(root, nil, &ExpandOptions{}))

	jazon = asJSON(t, root)

	assertRefInJSONRegexp(t, jazon, "^http://localhost:1234/tree$") // $ref now point to the root doc

	// a spec using the previous circular schema
	fixtureSpecPath := filepath.Join("fixtures", "more_circulars", "with-id.json")
	jazon, doc := expandThisOrDieTrying(t, fixtureSpecPath)

	assertRefInJSON(t, jazon, fixturePath) // all remaining $ref's point to the circular ID (http://...)

	// ResolveRef fails, because there are some remote $ref, but ResolveRefWithBasePath is successful
	assertRefResolve(t, jazon, "", doc, &ExpandOptions{})
	assertRefExpand(t, jazon, "", doc)
}

func TestCircular_RemoteExpandAzure(t *testing.T) {
	// local copy of : https://raw.githubusercontent.com/Azure/azure-rest-api-specs/master/specification/network/resource-manager/Microsoft.Network/stable/2020-04-01/publicIpAddress.json
	server := httptest.NewServer(http.FileServer(http.Dir("fixtures/azure")))
	defer server.Close()

	basePath := server.URL + "/publicIpAddress.json"
	jazon, sch := expandThisOrDieTrying(t, basePath)

	// check a pointer with escaped path
	pth1, err := ResolvePathItem(sch, MustCreateRef("#/paths/~1subscriptions~1%7BsubscriptionId%7D~1providers~1Microsoft.Network~1publicIPAddresses"), nil)
	require.NoError(t, err)
	require.NotNil(t, pth1)

	// check expected remaining $ref
	assertRefInJSONRegexp(t, jazon, `^(#/definitions/)|(networkInterface.json#/definitions/)|(networkSecurityGroup.json#/definitions/)|(network.json#/definitions)|(virtualNetworkTap.json#/definitions/)|(virtualNetwork.json#/definitions/)|(privateEndpoint.json#/definitions/)|(\./examples/)`)

	// check all $ref resolve in the expanded root
	// (filter out the remaining $ref in x-ms-example extensions, which are not expanded)
	t.Run("resolve $ref azure", func(t *testing.T) {
		assertRefResolve(t, jazon, `\./example`, sch, &ExpandOptions{RelativeBase: basePath})
	})
}
