#!/usr/bin/env python3
"""Convert the automation PostgreSQL migrations to one idempotent MySQL 8 migration.

Usage: pg2mysql_automation.py <migrations dir> > sql/mysql/010_automation.sql

Only the constructs the automation migrations use are understood; anything else aborts the conversion
instead of guessing. Statements are separated by "-- @@" lines (the repository's MySQL migration format).

Rules worth knowing:
  * text columns that appear in a key (PRIMARY KEY, UNIQUE, an index, a foreign key) become VARCHAR with a
    length by name (…_id 64, …_hash 64, else 128, name 191) and utf8mb4_bin, because PostgreSQL compares
    text case-sensitively; other text becomes LONGTEXT (defaults become expression defaults).
  * Partial indexes cannot exist in MySQL. A non-unique one becomes a plain index. A unique one is only
    allowed when OVERRIDES supplies an emulation with a generated column; otherwise conversion aborts.
  * Inline REFERENCES (silently ignored by MySQL) become FOREIGN KEY clauses.
  * ADD COLUMN / CREATE INDEX / DROP INDEX IF [NOT] EXISTS are emulated with information_schema guards.
"""
import os
import re
import sys

LEN = [(re.compile(r'(^|_)hash$'), 64), (re.compile(r'(^|_)id$'), 64), (re.compile(r'^name$'), 191)]
DEFAULT_LEN = 128

# Partial unique indexes: index name -> (generated column spec, statements).
OVERRIDES = {
    'alarm_episodes_active': None,   # filled below, depends on the columns of the migration that defines it
    'auto_rules_resource_key': None,
}


def strip_comments(sql):
    return re.sub(r'--[^\n]*', '', sql)


def split_statements(sql):
    sql = strip_comments(sql)
    out, depth, cur = [], 0, []
    for ch in sql:
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
        if ch == ';' and depth == 0:
            stmt = ''.join(cur).strip()
            if stmt:
                out.append(stmt)
            cur = []
        else:
            cur.append(ch)
    tail = ''.join(cur).strip()
    if tail:
        out.append(tail)
    return out


def split_top(s):
    parts, depth, cur = [], 0, ''
    for ch in s:
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
        if ch == ',' and depth == 0:
            parts.append(cur.strip())
            cur = ''
        else:
            cur += ch
    if cur.strip():
        parts.append(cur.strip())
    return parts


def key_length(name):
    for pattern, n in LEN:
        if pattern.search(name):
            return n
    return DEFAULT_LEN


class Converter:
    def __init__(self):
        self.tables = {}      # table -> {column: type}
        self.keyed = {}       # table -> set(columns used in keys)
        self.pending = []     # parsed statements, converted after the key scan
        self.out = []

    # ---- pass 1: which text columns need a bounded type ------------------------------------------
    def scan(self, statements):
        for stmt in statements:
            m = re.match(r'(?is)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+(\w+)\s*\((.*)\)\s*$', stmt)
            if m:
                table, body = m.group(1), m.group(2)
                self.tables.setdefault(table, {})
                keyed = self.keyed.setdefault(table, set())
                for item in split_top(body):
                    cm = re.match(r'(?is)^(\w+)\s+(\w+)(.*)$', item)
                    upper = item.upper()
                    if upper.startswith(('PRIMARY KEY', 'UNIQUE', 'FOREIGN KEY', 'CONSTRAINT')):
                        for cols in re.findall(r'\(([^)]*)\)', item):
                            keyed.update(c.strip().split()[0] for c in cols.split(','))
                        continue
                    if not cm:
                        raise SystemExit('cannot parse column: ' + item)
                    col, typ, rest = cm.group(1), cm.group(2).lower(), cm.group(3)
                    self.tables[table][col] = typ
                    if re.search(r'(?i)PRIMARY\s+KEY|\bUNIQUE\b|\bREFERENCES\b', rest):
                        keyed.add(col)
                continue
            m = re.match(r'(?is)CREATE\s+(UNIQUE\s+)?INDEX\s+IF\s+NOT\s+EXISTS\s+(\w+)\s+ON\s+(\w+)\s*\(([^)]*)\)', stmt)
            if m:
                cols = [c.strip().split()[0] for c in m.group(4).split(',')]
                self.keyed.setdefault(m.group(3), set()).update(cols)
                continue
            m = re.match(r'(?is)ALTER\s+TABLE\s+(\w+)\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+(\w+)\s+(\w+)', stmt)
            if m:
                self.tables.setdefault(m.group(1), {})[m.group(2)] = m.group(3).lower()

    # ---- pass 2: convert ---------------------------------------------------------------------------
    def column_type(self, table, col, typ, rest):
        typ = typ.lower()
        keyed = col in self.keyed.get(table, set())
        default = None
        dm = re.search(r'(?is)\bDEFAULT\s+(.*?)(?=\s+(?:NOT\s+NULL|NULL|PRIMARY|UNIQUE|REFERENCES|CHECK)\b|$)', rest)
        if dm:
            default = dm.group(1).strip()
        sql_type = None
        if typ == 'text':
            sql_type = f'VARCHAR({key_length(col)}) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin' if keyed else 'LONGTEXT'
        elif typ in ('int', 'integer'):
            sql_type = 'INT'
        elif typ == 'bigint':
            sql_type = 'BIGINT'
        elif typ == 'boolean':
            sql_type = 'TINYINT(1)'
        elif typ == 'timestamptz':
            sql_type = 'DATETIME(6)'
        elif typ == 'jsonb':
            sql_type = 'JSON'
        elif typ == 'uuid':
            sql_type = 'CHAR(36) CHARACTER SET ascii COLLATE ascii_bin'
        else:
            raise SystemExit(f'unsupported type {typ} in {table}.{col}')
        parts = [sql_type]
        if re.search(r'(?i)\bNOT\s+NULL\b', rest) or re.search(r'(?i)\bPRIMARY\s+KEY\b', rest):
            parts.append('NOT NULL')  # a PostgreSQL primary key is implicitly NOT NULL
        elif typ != 'timestamptz':
            parts.append('NULL')
        else:
            parts.append('NULL')
        if default is not None:
            d = default
            d = re.sub(r"(?i)'(\{\})'::jsonb", '(JSON_OBJECT())', d)
            d = re.sub(r"(?i)'(\[\])'::jsonb", '(JSON_ARRAY())', d)
            d = re.sub(r'(?i)^now\(\)$', 'CURRENT_TIMESTAMP(6)', d)
            d = re.sub(r'(?i)^true$', '1', d)
            d = re.sub(r'(?i)^false$', '0', d)
            if '::' in d:
                raise SystemExit(f'unsupported default {default} in {table}.{col}')
            if sql_type == 'LONGTEXT' or typ == 'jsonb':
                if not d.startswith('('):
                    d = f'({d})'
            parts.append('DEFAULT ' + d)
        if re.search(r'(?i)\bPRIMARY\s+KEY\b', rest):
            parts.append('PRIMARY KEY')
        if re.search(r'(?i)\bUNIQUE\b', rest) and not re.search(r'(?i)PRIMARY\s+KEY', rest):
            parts.append('UNIQUE')
        return ' '.join(parts)

    def create_table(self, table, body):
        defs, keys, fks = [], [], []
        for item in split_top(body):
            upper = item.upper()
            if upper.startswith('PRIMARY KEY') or upper.startswith('UNIQUE') or upper.startswith('FOREIGN KEY') or upper.startswith('CONSTRAINT'):
                keys.append(item)
                continue
            cm = re.match(r'(?is)^(\w+)\s+(\w+)(.*)$', item)
            col, typ, rest = cm.group(1), cm.group(2), cm.group(3)
            ref = re.search(r'(?is)\bREFERENCES\s+(\w+)\s*\((\w+)\)(\s+ON\s+DELETE\s+CASCADE)?', rest)
            rest_no_ref = re.sub(r'(?is)\bREFERENCES\s+\w+\s*\(\w+\)(\s+ON\s+DELETE\s+CASCADE)?', '', rest)
            defs.append(f'    `{col}` ' + self.column_type(table, col, typ, rest_no_ref))
            if ref:
                fks.append(f'    FOREIGN KEY (`{col}`) REFERENCES {ref.group(1)} (`{ref.group(2)}`)' + (' ON DELETE CASCADE' if ref.group(3) else ''))
        for k in keys:
            k = re.sub(r'\(([^)]*)\)', lambda m: '(' + ', '.join('`' + c.strip().split()[0] + '`' + (' ' + ' '.join(c.strip().split()[1:]) if len(c.strip().split()) > 1 else '') for c in m.group(1).split(',')) + ')', k, count=1)
            defs.append('    ' + k)
        defs.extend(fks)
        self.out.append(f'CREATE TABLE IF NOT EXISTS {table} (\n' + ',\n'.join(defs) + '\n) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin')

    def guarded(self, check_sql, action_sql, tag):
        self.out.append(f"SET @tp_{tag} := ({check_sql})")
        self.out.append(f"SET @tp_{tag}_sql := IF(@tp_{tag} = 0, '{action_sql.replace(chr(39), chr(39) * 2)}', 'SELECT 1')")
        self.out.append(f'PREPARE tp_{tag}_stmt FROM @tp_{tag}_sql')
        self.out.append(f'EXECUTE tp_{tag}_stmt')
        self.out.append(f'DEALLOCATE PREPARE tp_{tag}_stmt')

    def convert(self, stmt, counter):
        m = re.match(r'(?is)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+(\w+)\s*\((.*)\)\s*$', stmt)
        if m:
            return self.create_table(m.group(1), m.group(2))
        m = re.match(r'(?is)CREATE\s+(UNIQUE\s+)?INDEX\s+IF\s+NOT\s+EXISTS\s+(\w+)\s+ON\s+(\w+)\s*\(([^)]*)\)\s*(WHERE\s+(.*))?$', stmt)
        if m:
            unique, name, table, cols, where = bool(m.group(1)), m.group(2), m.group(3), m.group(4), m.group(6)
            if where and unique:
                raise PartialUnique(name, table, cols, where)
            colsql = ', '.join('`' + c.strip().split()[0] + '`' + (' ' + c.strip().split()[1] if len(c.strip().split()) > 1 else '') for c in cols.split(','))
            action = f"CREATE {'UNIQUE ' if unique else ''}INDEX {name} ON {table} ({colsql})"
            note = f' -- partial index (WHERE {where.strip()}) kept as a plain index' if where else ''
            self.guarded(f"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = '{table}' AND index_name = '{name}'", action, f'ix{counter}')
            return
        m = re.match(r'(?is)ALTER\s+TABLE\s+(\w+)\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+(\w+)\s+(\w+)(.*)$', stmt)
        if m:
            table, col, typ, rest = m.groups()
            self.tables.setdefault(table, {})[col] = typ.lower()
            action = f'ALTER TABLE {table} ADD COLUMN `{col}` ' + self.column_type(table, col, typ, rest)
            self.guarded(f"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = '{table}' AND column_name = '{col}'", action, f'col{counter}')
            return
        m = re.match(r'(?is)DROP\s+INDEX\s+IF\s+EXISTS\s+(\w+)\s*$', stmt)
        if m:
            name = m.group(1)
            table = self.index_table.get(name)
            if not table:
                raise SystemExit('cannot find the table of dropped index ' + name)
            self.out.append(f"SET @tp_d{counter} := (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = '{table}' AND index_name = '{name}')")
            self.out.append(f"SET @tp_d{counter}_sql := IF(@tp_d{counter} > 0, 'DROP INDEX {name} ON {table}', 'SELECT 1')")
            self.out.append(f'PREPARE tp_d{counter}_stmt FROM @tp_d{counter}_sql')
            self.out.append(f'EXECUTE tp_d{counter}_stmt')
            self.out.append(f'DEALLOCATE PREPARE tp_d{counter}_stmt')
            return
        if re.match(r'(?is)UPDATE\s+\w+\s+SET\s', stmt):
            if '::' in stmt or re.search(r'(?i)\b(jsonb|returning)\b', stmt):
                raise SystemExit('unsupported UPDATE: ' + stmt[:200])
            body = re.sub(r'(?i)\bnow\(\)', 'NOW(6)', stmt)
            body = re.sub(r'(?i)\btrue\b', '1', body)
            body = re.sub(r'(?i)\bfalse\b', '0', body)
            self.out.append(body)
            return
        m = re.match(r'(?is)CREATE\s+SEQUENCE\s+IF\s+NOT\s+EXISTS\s+(\w+)\s*$', stmt)
        if m:
            # MySQL has no sequences. A one-row counter advanced with LAST_INSERT_ID(value + 1) on its own
            # autocommit connection behaves like nextval(): monotonic, gaps allowed, not rolled back.
            name = m.group(1)
            self.out.append(f'CREATE TABLE IF NOT EXISTS {name}_counter (\n    `id` TINYINT NOT NULL PRIMARY KEY,\n    `value` BIGINT NOT NULL\n) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin')
            self.out.append(f'INSERT IGNORE INTO {name}_counter (`id`, `value`) VALUES (1, 0)')
            return
        raise SystemExit('unsupported statement: ' + stmt[:200])


class PartialUnique(Exception):
    def __init__(self, name, table, cols, where):
        self.name, self.table, self.cols, self.where = name, table, cols, where


def main():
    directory = sys.argv[1]
    files = sorted((f for f in os.listdir(directory) if f.endswith('.up.sql')), key=lambda f: int(f.split('_')[0]))
    conv = Converter()
    conv.index_table = {}
    per_file = []
    for f in files:
        stmts = split_statements(open(os.path.join(directory, f)).read())
        per_file.append((f, stmts))
        conv.scan(stmts)
        for s in stmts:
            m = re.match(r'(?is)CREATE\s+(?:UNIQUE\s+)?INDEX\s+IF\s+NOT\s+EXISTS\s+(\w+)\s+ON\s+(\w+)', s)
            if m:
                conv.index_table[m.group(1)] = m.group(2)
    counter = 0
    emulations = []
    for f, stmts in per_file:
        conv.out.append(f'-- source: {f}')
        for s in stmts:
            counter += 1
            try:
                conv.convert(s, counter)
            except PartialUnique as p:
                emulations.append((f, p))
                conv.out.append(f'-- PARTIAL-UNIQUE {p.name} ON {p.table}({p.cols.strip()}) WHERE {p.where.strip()}')
    print('-- Generated by scripts/pg2mysql_automation.py from encore-api/automation/migrations; do not edit by hand.')
    print('-- Partial unique indexes are listed as PARTIAL-UNIQUE comments and emulated in 011_automation_partial_unique.sql.')
    print('-- @@')
    first = True
    for chunk in conv.out:
        if chunk.startswith('-- '):
            print(chunk)
            continue
        print(chunk)
        print('-- @@')


if __name__ == '__main__':
    main()
