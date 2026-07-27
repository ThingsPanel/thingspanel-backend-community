package hints

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type IndexHint struct {
	Type string
	Keys []string
}

func (indexHint IndexHint) ModifyStatement(stmt *gorm.Statement) {
	for _, name := range []string{"FROM", "UPDATE"} {
		clause := stmt.Clauses[name]

		if clause.AfterExpression == nil {
			clause.AfterExpression = indexHint
		} else {
			clause.AfterExpression = Exprs{clause.AfterExpression, indexHint}
		}

		stmt.Clauses[name] = clause
	}
}

func (indexHint IndexHint) Build(builder clause.Builder) {
	if len(indexHint.Keys) > 0 {
		builder.WriteString(indexHint.Type)
		builder.WriteByte('(')
		for idx, key := range indexHint.Keys {
			if idx > 0 {
				builder.WriteByte(',')
			}
			builder.WriteQuoted(key)
		}
		builder.WriteByte(')')
	}
}

func UseIndex(names ...string) IndexHint {
	return IndexHint{Type: "USE INDEX ", Keys: names}
}

func IgnoreIndex(names ...string) IndexHint {
	return IndexHint{Type: "IGNORE INDEX ", Keys: names}
}

func ForceIndex(names ...string) IndexHint {
	return IndexHint{Type: "FORCE INDEX ", Keys: names}
}

func (indexHint IndexHint) ForJoin() IndexHint {
	indexHint.Type += "FOR JOIN "
	return indexHint
}

func (indexHint IndexHint) ForOrderBy() IndexHint {
	indexHint.Type += "FOR ORDER BY "
	return indexHint
}

func (indexHint IndexHint) ForGroupBy() IndexHint {
	indexHint.Type += "FOR GROUP BY "
	return indexHint
}
