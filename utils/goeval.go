package utils

import (
	"log"
	"strconv"
	"strings"

	"github.com/Knetic/govaluate"
	"github.com/PaulXu-cn/goeval"
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
		log.Fatal("syntax error:", err)
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		log.Fatal("evaluate error:", err)
	}

	return strconv.FormatBool(result.(bool))
}
