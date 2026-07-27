package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultResolutionCache(t *testing.T) {
	jsonSchema := MustLoadJSONSchemaDraft04()
	swaggerSchema := MustLoadSwagger20Schema()

	cache := defaultResolutionCache()

	sch, ok := cache.Get("not there")
	assert.False(t, ok)
	assert.Nil(t, sch)

	sch, ok = cache.Get("http://swagger.io/v2/schema.json")
	assert.True(t, ok)
	assert.Equal(t, swaggerSchema, sch)

	sch, ok = cache.Get("http://json-schema.org/draft-04/schema")
	assert.True(t, ok)
	assert.Equal(t, jsonSchema, sch)

	cache.Set("something", "here")
	sch, ok = cache.Get("something")
	assert.True(t, ok)
	assert.Equal(t, "here", sch)
}
