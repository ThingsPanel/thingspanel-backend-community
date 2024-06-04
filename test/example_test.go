/*
 * @Author: smith
 * @Date: 2024-3-7 22:15:38
 * @LastEditTime: 2024-3-7 22:15:38
 * @LastEditors: smith
 * @Description: In User Settings Edit
 * @FilePath: /irrigation-iot-platform/test/example_test.go
 * 单元测试示例
 */

package test

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestExample(t *testing.T) {
	assert := assert.New(t) //

	// assert equality
	assert.Equal(123, 123, "they should be equal")

	// assert inequality
	assert.NotEqual(123, 456, "they should not be equal")
	object := struct{ Value string }{}
	// assert for nil (good for errors)
	assert.NotNil(object)

	// assert for not nil (good when you expect something)
	// if assert.NotNil(object) {

	//   // now we know that object isn't nil, we are safe to make
	//   // further assertions without causing any errors
	//   assert.Equal("Something", object.Value)
	// }
}
