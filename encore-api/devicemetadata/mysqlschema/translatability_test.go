package mysqlschema

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

var sqlLiteral = regexp.MustCompile(`(?i)\b(select|insert\s+into|update|delete\s+from|create\s+table|alter\s+table)\b`)

var reasons = []struct {
	name string
	re   *regexp.Regexp
}{
	{"RETURNING", regexp.MustCompile(`(?i)\bRETURNING\b`)},
	{"conditional upsert", regexp.MustCompile(`(?is)ON\s+CONFLICT.*DO\s+UPDATE.*\bWHERE\b`)},
	{"= ANY / array function", regexp.MustCompile(`(?i)=\s*ANY\s*\(|unnest\(|array_agg\(|string_agg\(|generate_series\(`)},
	{"type cast on an expression", regexp.MustCompile(`::`)},
	{"jsonb function or column type", regexp.MustCompile(`(?i)jsonb|to_jsonb`)},
	{"PostgreSQL function", regexp.MustCompile(`(?i)\bpg_[a-z_]+|hashtext|current_schema`)},
	{"interval literal", regexp.MustCompile(`(?i)interval\s+'`)},
	{"ILIKE", regexp.MustCompile(`(?i)\bILIKE\b`)},
}

// Informational: how much of the automation SQL the MySQL translator already accepts, and why the rest
// is refused. It fails only if the scan finds no SQL at all, so a moved directory cannot pass silently.
func TestDeviceMetadataSQLTranslatabilityReport(t *testing.T) {
	root := filepath.Join("..")
	type tally struct{ total, ok int }
	perFile := map[string]*tally{}
	why := map[string]int{}
	var examples = map[string]string{}
	var refused []string
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != root {
				return filepath.SkipDir // only the service's own files, not accessconfig/middleware/mysqlschema
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, isLit := n.(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				return true
			}
			text, uerr := strconv.Unquote(lit.Value)
			if uerr != nil || !sqlLiteral.MatchString(text) {
				return true
			}
			name := strings.TrimPrefix(path, root+string(filepath.Separator))
			c := perFile[name]
			if c == nil {
				c = &tally{}
				perFile[name] = c
			}
			c.total++
			args := make([]any, 60)
			if _, _, ok := sqldialect.MySQL.Rewrite(text, args); ok {
				c.ok++
				return true
			}
			reason := "other"
			for _, r := range reasons {
				if r.re.MatchString(text) {
					reason = r.name
					break
				}
			}
			why[reason]++
			refused = append(refused, reason+" | "+strings.TrimPrefix(path, root+string(filepath.Separator))+" | "+strings.Join(strings.Fields(text), " "))
			if _, seen := examples[reason]; !seen {
				examples[reason] = strings.Join(strings.Fields(text), " ")
				if len(examples[reason]) > 140 {
					examples[reason] = examples[reason][:140] + "…"
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	total, ok := 0, 0
	var names []string
	for name, c := range perFile {
		total += c.total
		ok += c.ok
		names = append(names, name)
	}
	if total == 0 {
		t.Fatal("no SQL was found; the scan root is wrong")
	}
	sort.Strings(names)
	t.Logf("devicemetadata SQL literals: %d, translated by the MySQL translator: %d (%.0f%%)", total, ok, 100*float64(ok)/float64(total))
	for _, name := range names {
		if c := perFile[name]; c.ok != c.total {
			t.Logf("  %-40s %3d/%3d translate", name, c.ok, c.total)
		}
	}
	if dump := os.Getenv("TP_DM_REFUSED_FILE"); dump != "" {
		sort.Strings(refused)
		_ = os.WriteFile(dump, []byte(strings.Join(refused, "\n")+"\n"), 0o644)
	}
	var keys []string
	for k := range why {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return why[keys[i]] > why[keys[j]] })
	for _, k := range keys {
		t.Logf("  refused because of %-32s %3d   e.g. %s", k, why[k], examples[k])
	}
}
