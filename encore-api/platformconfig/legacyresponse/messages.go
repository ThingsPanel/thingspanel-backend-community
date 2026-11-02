// Package legacyresponse loads the existing response-message asset. It is a
// presentation adapter, with no Gin dependency or dictionary business rules.
package legacyresponse

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Catalog struct{ messages map[int]map[string]string }

func Load(path string) (*Catalog, error) {
	contents, err := os.ReadFile(path)
	if err != nil || len(contents) > 1<<20 {
		return nil, fmt.Errorf("dictionary response messages are unavailable")
	}
	var input struct {
		Messages map[int]map[string]string `yaml:"messages"`
	}
	if yaml.Unmarshal(contents, &input) != nil {
		return nil, fmt.Errorf("dictionary response messages are invalid")
	}
	for _, code := range []int{200, 100000, 100001, 100002, 101001, 201001, 201002} {
		if input.Messages[code]["zh_CN"] == "" || input.Messages[code]["en_US"] == "" {
			return nil, fmt.Errorf("dictionary response messages are incomplete")
		}
	}
	return &Catalog{messages: input.Messages}, nil
}

// Message retains the established errcode manager's response-language rule.
// This is intentionally distinct from the enum's FormatLangCode rule: an empty
// header means zh_CN for messages and en_US for dictionary translations.
func (c *Catalog) Message(code int, header string) string {
	if c == nil {
		return "未知错误"
	}
	available := c.messages[code]
	lookup := func(language string) string {
		if value := available[language]; value != "" {
			return value
		}
		if language == "zh_CN" {
			return "未知错误"
		}
		return ""
	}
	type language struct {
		tag    string
		weight float64
	}
	var languages []language
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ";")
		item := language{tag: strings.TrimSpace(fields[0]), weight: 1}
		if len(fields) > 1 {
			q := strings.TrimSpace(fields[1])
			if strings.HasPrefix(q, "q=") {
				if weight, err := strconv.ParseFloat(strings.TrimPrefix(q, "q="), 64); err == nil {
					item.weight = weight
				}
			}
		}
		languages = append(languages, item)
	}
	sort.Slice(languages, func(i, j int) bool { return languages[i].weight > languages[j].weight })
	for _, language := range languages {
		if value := lookup(strings.ReplaceAll(language.tag, "-", "_")); value != "" {
			return value
		}
	}
	return lookup("zh_CN")
}
