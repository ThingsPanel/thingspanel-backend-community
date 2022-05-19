package utils

import (
	"strconv"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/PaulXu-cn/goeval"
	"github.com/beego/beego/v2/core/logs"
)

func EvalOld(code string) string {
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

func Eval(code string) string {
	expr, err := govaluate.NewEvaluableExpression(code)
	if err != nil {
		logs.Error("syntax error:", err)
		return strconv.FormatBool(false)
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		logs.Error("evaluate error:", err)
		return strconv.FormatBool(false)
	}

	return strconv.FormatBool(result.(bool))
}
