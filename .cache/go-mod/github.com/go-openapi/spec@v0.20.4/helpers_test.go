package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/go-openapi/swag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var rex = regexp.MustCompile(`"\$ref":\s*"(.*?)"`)

func jsonDoc(path string) (json.RawMessage, error) {
	data, err := swag.LoadFromFileOrHTTP(path)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func docAndOpts(t testing.TB, fixturePath string) ([]byte, *ExpandOptions) {
	doc, err := jsonDoc(fixturePath)
	require.NoError(t, err)

	return doc, &ExpandOptions{
		RelativeBase: fixturePath,
	}
}

func expandThisSchemaOrDieTrying(t testing.TB, fixturePath string) (string, *Schema) {
	doc, opts := docAndOpts(t, fixturePath)

	sch := new(Schema)
	require.NoError(t, json.Unmarshal(doc, sch))

	require.NotPanics(t, func() {
		require.NoError(t, ExpandSchemaWithBasePath(sch, nil, opts))
	}, "calling expand schema circular refs, should not panic!")

	bbb, err := json.MarshalIndent(sch, "", " ")
	require.NoError(t, err)

	return string(bbb), sch
}

func expandThisOrDieTrying(t testing.TB, fixturePath string) (string, *Swagger) {
	doc, opts := docAndOpts(t, fixturePath)

	spec := new(Swagger)
	require.NoError(t, json.Unmarshal(doc, spec))

	require.NotPanics(t, func() {
		require.NoError(t, ExpandSpec(spec, opts))
	}, "calling expand spec with circular refs, should not panic!")

	bbb, err := json.MarshalIndent(spec, "", " ")
	require.NoError(t, err)

	return string(bbb), spec
}

// assertRefInJSONRegexp ensures all $ref in a jazon document have a given prefix.
//
// NOTE: matched $ref might be empty.
func assertRefInJSON(t testing.TB, jazon, prefix string) {
	// assert a match in a references
	m := rex.FindAllStringSubmatch(jazon, -1)
	require.NotNil(t, m)

	for _, matched := range m {
		subMatch := matched[1]
		assert.True(t, strings.HasPrefix(subMatch, prefix),
			"expected $ref to match %q, got: %s", prefix, matched[0])
	}
}

// assertRefInJSONRegexp ensures all $ref in a jazon document match a given regexp
//
// NOTE: matched $ref might be empty.
func assertRefInJSONRegexp(t testing.TB, jazon, match string) {
	// assert a match in a references
	m := rex.FindAllStringSubmatch(jazon, -1)
	require.NotNil(t, m)

	refMatch, err := regexp.Compile(match)
	require.NoError(t, err)

	for _, matched := range m {
		subMatch := matched[1]
		assert.True(t, refMatch.MatchString(subMatch),
			"expected $ref to match %q, got: %s", match, matched[0])
	}
}

// assertNoRef ensures that no $ref is remaining in json doc
func assertNoRef(t testing.TB, jazon string) {
	m := rex.FindAllStringSubmatch(jazon, -1)
	require.Nil(t, m)
}

// assertRefExpand ensures that all $ref in some json doc expand properly against a root document.
//
// "exclude" is a regexp pattern to ignore certain $ref (e.g. some specs may embed $ref that are not processed, such as extensions).
func assertRefExpand(t *testing.T, jazon, exclude string, root interface{}, opts ...*ExpandOptions) {
	assertRefWithFunc(t, jazon, "", func(t *testing.T, match string) {
		ref := RefSchema(match)
		if len(opts) > 0 {
			options := *opts[0]
			require.NoError(t, ExpandSchemaWithBasePath(ref, nil, &options))
		} else {
			require.NoError(t, ExpandSchema(ref, root, nil))
		}
	})
}

// assertRefResolve ensures that all $ref in some json doc resolve properly against a root document.
//
// "exclude" is a regexp pattern to ignore certain $ref (e.g. some specs may embed $ref that are not processed, such as extensions).
func assertRefResolve(t *testing.T, jazon, exclude string, root interface{}, opts ...*ExpandOptions) {
	assertRefWithFunc(t, jazon, exclude, func(t *testing.T, match string) {
		ref := MustCreateRef(match)
		var (
			sch *Schema
			err error
		)
		if len(opts) > 0 {
			options := *opts[0]
			sch, err = ResolveRefWithBase(root, &ref, &options)
		} else {
			sch, err = ResolveRef(root, &ref)
		}

		require.NoErrorf(t, err, `%v: for "$ref": %q`, err, match)
		require.NotNil(t, sch)
	})
}

// assertRefResolve ensures that all $ref in some json doc verify some asserting func.
//
// "exclude" is a regexp pattern to ignore certain $ref (e.g. some specs may embed $ref that are not processed, such as extensions).
func assertRefWithFunc(t *testing.T, jazon, exclude string, asserter func(t *testing.T, match string)) {
	filterRex := regexp.MustCompile(exclude)
	m := rex.FindAllStringSubmatch(jazon, -1)
	require.NotNil(t, m)
	allRefs := make(map[string]struct{}, len(m))
	for _, matched := range m {
		subMatch := matched[1]
		if exclude != "" && filterRex.MatchString(subMatch) {
			continue
		}
		_, ok := allRefs[subMatch]
		if ok {
			continue
		}
		allRefs[subMatch] = struct{}{}

		t.Run(fmt.Sprintf("%s-%s", t.Name(), subMatch), func(t *testing.T) {
			t.Parallel()
			asserter(t, subMatch)
		})
	}
}

func asJSON(t testing.TB, sp interface{}) string {
	bbb, err := json.MarshalIndent(sp, "", " ")
	require.NoError(t, err)

	return string(bbb)
}
