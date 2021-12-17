package utils

import (
	"strings"

	"github.com/PaulXu-cn/goeval"
)

func Eval(code string) string {
	template := `
		var flag = ${code}
		fmt.Print(flag)
	`
	out := strings.Replace(template, "${code}", code, -1)
	if re, err := goeval.Eval("", out, "fmt"); nil == err {
		return string(re)
	} else {
		return "error"
	}
}
