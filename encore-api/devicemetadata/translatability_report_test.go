package devicemetadata

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"encore.app/pkg/sqldialect"
)

var metadataSQLLiteral = regexp.MustCompile(`(?i)\b(select|insert\s+into|update|delete\s+from|create\s+table|alter\s+table)\b`)

var metadataSQLBlockers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"RETURNING", regexp.MustCompile(`(?i)\bRETURNING\b`)},
	{"conditional upsert", regexp.MustCompile(`(?is)ON\s+CONFLICT.*DO\s+UPDATE.*\bWHERE\b`)},
	{"array operation", regexp.MustCompile(`(?i)=\s*ANY\s*\(|unnest\(|array_agg\(|string_agg\(|generate_series\(`)},
	{"type cast", regexp.MustCompile(`::`)},
	{"JSON function or jsonb", regexp.MustCompile(`(?i)jsonb|to_jsonb`)},
	{"PostgreSQL function", regexp.MustCompile(`(?i)\bpg_[a-z_]+|hashtext|current_schema`)},
	{"interval literal", regexp.MustCompile(`(?i)interval\s+'`)},
	{"ILIKE", regexp.MustCompile(`(?i)\bILIKE\b`)},
	{"window function", regexp.MustCompile(`(?i)\bOVER\s*\(`)},
	{"other", regexp.MustCompile(`(?s).`)},
}

// TestMySQLSQLTranslatabilityReport is diagnostic only: a translated literal has not thereby been proven to run
// correctly against the MySQL schema, and dynamic SQL is outside this static scan.
func TestMySQLSQLTranslatabilityReport(t *testing.T) {
	type tally struct{ total, translated int }
	perFile := map[string]*tally{}
	blockers := map[string]int{}
	examples := map[string]string{}
	fset := token.NewFileSet()
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			query, err := strconv.Unquote(literal.Value)
			if err != nil || !metadataSQLLiteral.MatchString(query) {
				return true
			}
			count := perFile[path]
			if count == nil {
				count = &tally{}
				perFile[path] = count
			}
			count.total++
			if _, _, ok := sqldialect.MySQL.Rewrite(query, make([]any, 64)); ok {
				count.translated++
				return true
			}
			for _, blocker := range metadataSQLBlockers {
				if blocker.re.MatchString(query) {
					blockers[blocker.name]++
					if _, exists := examples[blocker.name]; !exists {
						examples[blocker.name] = strings.Join(strings.Fields(query), " ")
						if len(examples[blocker.name]) > 140 {
							examples[blocker.name] = examples[blocker.name][:140] + "…"
						}
					}
					break
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	total, translated := 0, 0
	var files []string
	for path, count := range perFile {
		total += count.total
		translated += count.translated
		files = append(files, path)
	}
	if total == 0 {
		t.Fatal("no SQL literals found in devicemetadata source")
	}
	sort.Strings(files)
	t.Logf("devicemetadata SQL literals: %d; translator accepts %d (%.0f%%)", total, translated, 100*float64(translated)/float64(total))
	for _, path := range files {
		if count := perFile[path]; count.translated != count.total {
			t.Logf("  %s: %d/%d translate", path, count.translated, count.total)
		}
	}
	var names []string
	for name := range blockers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return blockers[names[i]] > blockers[names[j]] })
	for _, name := range names {
		t.Logf("  refused: %-28s %3d; e.g. %s", name, blockers[name], examples[name])
	}
}
