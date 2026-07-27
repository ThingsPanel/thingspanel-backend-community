package hints

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Hints struct {
	Prefix  string
	Suffix  string
	Content string

	clauses []string
	before  bool
	after   bool
}

func (hints Hints) ModifyStatement(stmt *gorm.Statement) {
	for _, name := range hints.clauses {
		name = strings.ToUpper(name)
		clause := stmt.Clauses[name]
		switch {
		case hints.before:
			if clause.BeforeExpression == nil {
				clause.BeforeExpression = hints
			} else if old, ok := clause.BeforeExpression.(Hints); ok {
				old.Merge(hints)
				clause.BeforeExpression = old
			} else {
				clause.BeforeExpression = Exprs{clause.BeforeExpression, hints}
			}
		case hints.after:
			if clause.AfterExpression == nil {
				clause.AfterExpression = hints
			} else if old, ok := clause.AfterExpression.(Hints); ok {
				old.Merge(hints)
				clause.AfterExpression = old
			} else {
				clause.AfterExpression = Exprs{clause.AfterExpression, hints}
			}
		default:
			if clause.AfterNameExpression == nil {
				clause.AfterNameExpression = hints
			} else if old, ok := clause.AfterNameExpression.(Hints); ok {
				old.Merge(hints)
				clause.AfterNameExpression = old
			} else {
				clause.AfterNameExpression = Exprs{clause.AfterNameExpression, hints}
			}
		}

		stmt.Clauses[name] = clause
	}
}

func (hints Hints) Build(builder clause.Builder) {
	builder.WriteString(hints.Prefix)
	builder.WriteString(hints.Content)
	builder.WriteString(hints.Suffix)
}

func (hints Hints) Merge(h Hints) {
	hints.Content += " " + h.Content
}

func New(content string) Hints {
	return Hints{Prefix: "/*+ ", Content: content, Suffix: " */", clauses: []string{"SELECT", "UPDATE"}}
}

func Comment(clause string, comment string) Hints {
	return Hints{clauses: []string{clause}, Prefix: "/* ", Content: comment, Suffix: " */"}
}

func CommentBefore(clause string, comment string) Hints {
	return Hints{clauses: []string{clause}, before: true, Prefix: "/* ", Content: comment, Suffix: " */"}
}

func CommentAfter(clause string, comment string) Hints {
	return Hints{clauses: []string{clause}, after: true, Prefix: "/* ", Content: comment, Suffix: " */"}
}
