#!/usr/bin/env python3
"""Generate sql/mysql/012_platform_base.sql from the final PostgreSQL platform schema.

The root migrations (sql/1.sql ... sql/32.sql) mix DDL, ALTERs with USING, DO blocks and seed data, so they are
not translated statement by statement. Instead they are applied to a scratch PostgreSQL and the *resulting*
catalog is converted. Anything the converter does not understand aborts the run.

Usage: pg2mysql_platform.py "<psql command prefix, e.g. docker exec -i NAME psql -U postgres>" [--standalone] > out.sql
(--standalone: an owner with its own database, e.g. device_metadata; nothing is excluded.)
For the platform: > sql/mysql/012_platform_base.sql

Tables already created by the hand-written MySQL migrations (001-011) are skipped; casbin_rule is created by the
casbin adapter. Everything else, including telemetry_datas as a plain relational history table, is generated.

Rules: every string column is utf8mb4_bin (PostgreSQL compares case-sensitively); text in a key becomes
VARCHAR(191); timestamp/timestamptz become DATETIME(6); json/jsonb become JSON; uuid CHAR(36); serials
AUTO_INCREMENT; partial indexes become plain indexes (a partial unique index aborts); CHECK constraints are
reported, not converted; foreign keys are emitted last, guarded by information_schema.
"""
import json
import re
import shlex
import subprocess
import sys

BIG_KEY = 191

Q = r"""
SELECT json_agg(t ORDER BY t.name) FROM (
 SELECT c.relname AS name,
  (SELECT json_agg(json_build_object('name',a.attname,'type',format_type(a.atttypid,a.atttypmod),'notnull',a.attnotnull,
          'default',pg_get_expr(d.adbin,d.adrelid),'generated',a.attgenerated::text) ORDER BY a.attnum)
     FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
    WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped) AS cols,
  (SELECT json_agg(json_build_object('name',i.relname,'unique',x.indisunique,'primary',x.indisprimary,'def',pg_get_indexdef(x.indexrelid))
          ORDER BY i.relname)
     FROM pg_index x JOIN pg_class i ON i.oid=x.indexrelid WHERE x.indrelid=c.oid) AS idx,
  (SELECT json_agg(json_build_object('name',k.conname,'def',pg_get_constraintdef(k.oid),'type',k.contype) ORDER BY k.conname)
     FROM pg_constraint k WHERE k.conrelid=c.oid) AS cons
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind='r') t
"""


def fetch(prefix):
    out = subprocess.run(shlex.split(prefix) + ['-At', '-c', Q], capture_output=True, text=True, check=True).stdout
    return json.loads(out)


def split_top(s):
    parts, depth, cur, quote = [], 0, '', False
    for ch in s:
        if ch == "'":
            quote = not quote
        if not quote:
            if ch == '(':
                depth += 1
            elif ch == ')':
                depth -= 1
            elif ch == ',' and depth == 0:
                parts.append(cur.strip())
                cur = ''
                continue
        cur += ch
    if cur.strip():
        parts.append(cur.strip())
    return parts


def parse_type(pg):
    pg = pg.strip()
    m = re.match(r'(character varying|varchar)\((\d+)\)$', pg)
    if m:
        return ('varchar', int(m.group(2)))
    m = re.match(r'character\((\d+)\)$', pg)
    if m:
        return ('char', int(m.group(1)))
    simple = {
        'text': ('text', 0), 'character varying': ('text', 0), 'integer': ('int', 0), 'smallint': ('smallint', 0),
        'bigint': ('bigint', 0), 'boolean': ('bool', 0), 'double precision': ('double', 0), 'real': ('float', 0),
        'uuid': ('uuid', 0), 'json': ('json', 0), 'jsonb': ('json', 0), 'bytea': ('blob', 0), 'date': ('date', 0),
        'timestamp with time zone': ('dt', 0), 'timestamp without time zone': ('dt', 0),
        'time without time zone': ('time', 0), 'time with time zone': ('time', 0),
    }
    if pg in simple:
        return simple[pg]
    m = re.match(r'timestamp\(\d+\) (with|without) time zone$', pg)
    if m:
        return ('dt', 0)
    m = re.match(r'time\(\d+\) (with|without) time zone$', pg)
    if m:
        return ('time', 0)
    m = re.match(r'numeric\((\d+),(\d+)\)$', pg)
    if m:
        return ('decimal', (int(m.group(1)), int(m.group(2))))
    if pg == 'numeric':
        return ('decimal', (38, 10))
    raise SystemExit(f'unsupported column type: {pg}')


STR = {'varchar', 'char', 'text'}


def mysql_type(kind, size, in_key):
    if kind == 'varchar':
        return f'VARCHAR({size}) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin'
    if kind == 'char':
        return f'CHAR({size}) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin'
    if kind == 'text':
        if in_key:
            return f'VARCHAR({BIG_KEY}) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin'
        return 'LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin'
    return {'int': 'INT', 'smallint': 'SMALLINT', 'bigint': 'BIGINT', 'bool': 'TINYINT(1)', 'double': 'DOUBLE', 'float': 'FLOAT',
            'uuid': 'CHAR(36) CHARACTER SET ascii COLLATE ascii_bin', 'json': 'JSON', 'blob': 'VARBINARY(255)' if in_key else 'LONGBLOB', 'date': 'DATE',
            'dt': 'DATETIME(6)', 'time': 'TIME'}.get(kind) or f'DECIMAL({size[0]},{size[1]})'


def convert_default(expr, kind, report, where):
    if expr is None:
        return None, False
    e = expr.strip()
    if e.startswith('nextval('):
        return None, True
    if re.fullmatch(r'(now\(\)|CURRENT_TIMESTAMP|LOCALTIMESTAMP)', e, re.I):
        return 'CURRENT_TIMESTAMP(6)', False
    if re.fullmatch(r'(gen_random_uuid\(\)|uuid_generate_v4\(\))', e, re.I):
        return '(UUID())', False
    m = re.fullmatch(r"'((?:[^']|'')*)'(?:::[a-z ]+(?:\(\d+\))?(?:\[\])?)?", e)
    if m:
        lit = "'" + m.group(1) + "'"
        if kind in ('text', 'json', 'blob'):
            return f'({lit})', False
        if kind == 'bool':
            return ('1' if m.group(1).lower() in ('t', 'true', '1') else '0'), False
        return lit, False
    if re.fullmatch(r'-?\d+(\.\d+)?', e):
        return e, False
    if re.fullmatch(r"\(?-?\d+(\.\d+)?\)?(::[a-z ]+)?", e):
        return re.sub(r'::.*', '', e).strip('()'), False
    if e.lower() in ('true', 'false'):
        return ('1' if e.lower() == 'true' else '0'), False
    report.append(f'{where}: default {expr!r} not converted (column left without default)')
    return None, False


MAX_KEY_BYTES = 3072


def key_bytes(cols, colinfo, in_key_text=BIG_KEY):
    """Bytes an InnoDB index entry needs for these plain columns (utf8mb4 strings take 4 per character)."""
    total = 0
    for c in cols:
        kind, size = colinfo[c]
        if kind == 'varchar':
            total += 4 * size
        elif kind == 'char':
            total += 4 * size
        elif kind == 'text':
            total += 4 * in_key_text
        elif kind == 'uuid':
            total += 36
        elif kind in ('bigint', 'dt', 'double'):
            total += 8
        elif kind in ('int', 'float', 'date'):
            total += 4
        elif kind == 'smallint':
            total += 2
        elif kind == 'bool':
            total += 1
        elif kind == 'time':
            total += 6
        else:
            total += 16
    return total


def split_index_columns(defn):
    start = re.search(r'USING \w+ \(', defn)
    if not start:
        raise SystemExit(f'cannot parse index: {defn}')
    depth, i = 1, start.end()
    while depth:
        depth += {'(': 1, ')': -1}.get(defn[i], 0)
        i += 1
    inner = defn[start.end():i - 1]
    where = re.match(r'\s*WHERE (.*)$', defn[i:], re.S)

    class M:
        def group(self, n):
            return inner if n == 1 else (where.group(1) if where else None)
    m = M()
    cols = []
    for part in split_top(m.group(1)):
        part = re.sub(r'\s+(varchar|text|bpchar)_pattern_ops\b', '', part)
        desc = bool(re.search(r'\s+DESC\b', part))
        part = re.sub(r'\s+(DESC|ASC)\b', '', part)
        part = re.sub(r'\s+NULLS (FIRST|LAST)\b', '', part)
        cols.append((part.strip(), desc))
    return cols, m.group(2)


def hashed_unique(table, name, cols, notnull, colinfo):
    """A unique key too long for InnoDB becomes a unique key on a stored SHA-256 of its columns."""
    for c in cols:
        if not notnull[c]:
            raise SystemExit(f'{table}.{name}: over-long unique key with nullable column {c} needs a manual emulation')
    expr = 'SHA2(CONCAT_WS(CHAR(31), ' + ', '.join('`' + c + '`' for c in cols) + '), 256)'
    return [f"    `{name}__uk` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin GENERATED ALWAYS AS ({expr}) STORED",
            f"    UNIQUE KEY `{name}` (`{name}__uk`)"]


class Unsupported(Exception):
    """An expression the converter cannot express in MySQL."""


def convert_expr(e, label):
    """A PostgreSQL boolean expression as MySQL: casts dropped, = ANY(ARRAY[..]) as IN, and the string, regex and
    JSON functions the metadata schemas use mapped to their MySQL equivalents. Anything left raises Unsupported."""
    e = re.sub(r"'(\{\})'::jsonb", r"CAST('\1' AS JSON)", e)
    e = re.sub(r"::(character varying|text|timestamp with time zone|timestamp without time zone|bigint|integer|smallint|uuid|jsonb|boolean|numeric)(\[\])?", '', e)
    e = re.sub(r'=\s*ANY\s*\(\s*\(\s*ARRAY\[(.*?)\]\s*\)\s*\)', r'IN (\1)', e, flags=re.S)
    e = re.sub(r'=\s*ANY\s*\(\s*ARRAY\[(.*?)\]\s*\)', r'IN (\1)', e, flags=re.S)
    e = re.sub(r'\boctet_length\(', 'LENGTH(', e)
    e = re.sub(r'\blength\(', 'CHAR_LENGTH(', e)
    e = re.sub(r'\bchr\((\d+)\)', r'CHAR(\1 USING utf8mb4)', e)
    e = re.sub(r'\bjsonb_typeof\(([^()]*)\)\s*=\s*\'object\'', r"JSON_TYPE(\1) = 'OBJECT'", e)
    for operator, template in (('!~', "NOT REGEXP_LIKE(%s, %s, 'c')"), ('~', "REGEXP_LIKE(%s, %s, 'c')")):
        e = re.sub(r"\((\w+)\)\s*" + operator + r"\s*('(?:[^']|'')*')", lambda m: template % (m.group(1), m.group(2)), e)
        e = re.sub(r"\b(\w+)\s*" + operator + r"\s*('(?:[^']|'')*')", lambda m: template % (m.group(1), m.group(2)), e)
    e = re.sub(r'\bnum_nonnulls\(([^()]*)\)', lambda m: '(' + ' + '.join(f'({a.strip()} IS NOT NULL)' for a in m.group(1).split(',')) + ')', e)
    e = e.replace(' ~~ ', ' LIKE ')
    # btrim(x) trims spaces, like TRIM(x); the two-argument form (a character set) has no equivalent.
    e = re.sub(r'\bbtrim\(([^(),]*(?:\([^()]*\))?[^(),]*)\)', r'TRIM(\1)', e)
    allowed = {'CHAR_LENGTH', 'LENGTH', 'TRIM', 'CHAR', 'JSON_TYPE', 'REGEXP_LIKE', 'CAST', 'IF', 'COALESCE', 'POSITION',
               'NOT', 'IN', 'AND', 'OR', 'IS', 'WHEN', 'THEN', 'ELSE', 'CASE', 'END', 'NULL', 'NULLIF', 'JSON', 'SHA2', 'CONCAT_WS', 'LOWER', 'UPPER', 'ABS'}
    called = {name.upper() for name in re.findall(r"\b([A-Za-z_]\w*)\s*\(", re.sub(r"'(?:[^']|'')*'", "''", e))}
    unknown = sorted(called - allowed)
    if unknown or '::' in e or re.search(r'\bANY\b|\bARRAY\b|~', re.sub(r"'(?:[^']|'')*'", "''", e)):
        raise Unsupported(f'{label}: {e} (unsupported: {unknown})')
    return e


def partial_unique(table, name, cols, where):
    """A partial unique index becomes a unique key on a generated column that is NULL (never conflicting, as in
    PostgreSQL) for rows outside the predicate or with a NULL key column, and a hash of the key otherwise."""
    predicate = convert_expr(where, f'{table}.{name} predicate')
    not_null = ' AND '.join(f'`{c}` IS NOT NULL' for c in cols)
    expr = f"IF(({predicate}) AND {not_null}, SHA2(CONCAT_WS(CHAR(31), " + ', '.join('`' + c + '`' for c in cols) + "), 256), NULL)"
    return [f"    `{name}__uk` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin GENERATED ALWAYS AS ({expr}) STORED",
            f"    UNIQUE KEY `{name}` (`{name}__uk`)"]


def convert_check(defn, table, name):
    m = re.match(r'CHECK \((.*)\)(?: NOT VALID)?$', defn, re.S)
    if not m:
        raise SystemExit(f'cannot parse CHECK {name} on {table}: {defn}')
    return convert_expr(m.group(1), f'CHECK {name} on {table}')


def hand_written_tables():
    """Names of the tables the hand-written MySQL migrations 001-011 create."""
    import glob
    import os
    names = set()
    here = os.path.dirname(os.path.abspath(__file__))
    for path in glob.glob(os.path.join(here, '..', 'sql', 'mysql', '0[01][0-9]_*.sql')):
        base = os.path.basename(path)
        if int(base[:3]) > 11:
            continue
        names.update(re.findall(r'CREATE TABLE IF NOT EXISTS\s+`?(\w+)`?', open(path).read(), re.I))
    return names


def main():
    prefix = sys.argv[1]
    if len(sys.argv) > 2 and sys.argv[2] == '--standalone':
        exclude = set()   # an owner with its own database: nothing is hand-written, nothing is skipped
    else:
        exclude = hand_written_tables()
        exclude.add('casbin_rule')
    tables = [t for t in fetch(prefix) if t['name'] not in exclude]
    names = {t['name'] for t in tables}
    report, stmts, fk_stmts = [], [], []
    deps = {}
    for t in tables:
        deps[t['name']] = set()
        for k in t['cons'] or []:
            if k['type'] == 'f':
                m = re.search(r'REFERENCES (?:public\.)?"?(\w+)"?', k['def'])
                if m and m.group(1) in names and m.group(1) != t['name']:
                    deps[t['name']].add(m.group(1))
    order, done = [], set()

    def visit(n, stack=()):
        if n in done or n in stack:
            return
        for d in sorted(deps[n]):
            visit(d, stack + (n,))
        if n not in done:
            done.add(n)
            order.append(n)
    for n in sorted(names):
        visit(n)
    by = {t['name']: t for t in tables}
    created = set()
    used_names = set()

    def unique_name(table, name):
        # MySQL constraint names are unique per database, PostgreSQL's per table.
        final = name if name not in used_names else f'{table}_{name}'
        used_names.add(final)
        return final
    counter = 0
    for n in order:
        t = by[n]
        keycols = set()
        for ix in t['idx'] or []:
            for c, _ in split_index_columns(ix['def'])[0]:
                keycols.add(c.strip('"'))
        for k in t['cons'] or []:
            if k['type'] == 'f':
                m = re.match(r'FOREIGN KEY \((.*?)\) REFERENCES', k['def'])
                keycols.update(c.strip().strip('"') for c in m.group(1).split(','))
        colinfo = {c['name']: parse_type(c['type']) for c in t['cols']}
        notnull = {c['name']: c['notnull'] for c in t['cols']}
        lines, pk_auto = [], None
        serial = set()
        for c in t['cols']:
            kind, size = parse_type(c['type'])
            if c.get('generated') == 's':
                # a stored generated column: its expression is in pg_attrdef like a default
                expr = convert_expr(c['default'], f"{n}.{c['name']} generated expression")
                ty = mysql_type(kind, size, c['name'] in keycols)
                lines.append(f"    `{c['name']}` {ty} GENERATED ALWAYS AS ({expr}) STORED{' NOT NULL' if c['notnull'] else ''}")
                continue
            default, is_serial = convert_default(c['default'], kind, report, f"{n}.{c['name']}")
            if is_serial:
                serial.add(c['name'])
            ty = mysql_type(kind, size, c['name'] in keycols)
            if kind == 'dt' and default is None and not is_serial and c['default'] is None:
                pass
            line = f"    `{c['name']}` {ty}{' NOT NULL' if c['notnull'] or is_serial else ' NULL'}"
            if is_serial:
                line += ' AUTO_INCREMENT'
            elif default is not None:
                line += f' DEFAULT {default}'
            lines.append(line)
        for k in t['cons'] or []:
            if k['type'] == 'p':
                pk_cols = [c.strip().strip('"') for c in re.match(r'PRIMARY KEY \((.*)\)', k['def']).group(1).split(',')]
                if key_bytes(pk_cols, colinfo) > MAX_KEY_BYTES:
                    # InnoDB takes a STORED generated column as the primary key: the key is a hash of the real columns,
                    # and a prefix index keeps lookups by those columns fast.
                    expr = 'SHA2(CONCAT_WS(CHAR(31), ' + ', '.join('`' + c + '`' for c in pk_cols) + '), 256)'
                    lines.append(f"    `{n}__pk` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin GENERATED ALWAYS AS ({expr}) STORED NOT NULL")
                    lines.append(f"    PRIMARY KEY (`{n}__pk`)")
                    prefixed = []
                    for c in pk_cols:
                        kind, size = colinfo[c]
                        prefixed.append(f'`{c}`(64)' if kind in ('varchar', 'char', 'text') and (size or BIG_KEY) > 64 else f'`{c}`')
                    lines.append(f"    KEY `{n}__pk_lookup` (" + ', '.join(prefixed) + ')')
                    report.append(f"{n}: primary key over {MAX_KEY_BYTES} bytes; carried by a stored hash column with a prefix index")
                else:
                    lines.append('    PRIMARY KEY (' + ', '.join('`' + c + '`' for c in pk_cols) + ')')
            elif k['type'] == 'u':
                cols = [c.strip().strip('"') for c in re.match(r'UNIQUE \((.*)\)', k['def']).group(1).split(',')]
                if key_bytes(cols, colinfo) > MAX_KEY_BYTES:
                    lines.extend(hashed_unique(n, k['name'], cols, notnull, colinfo))
                else:
                    lines.append(f"    UNIQUE KEY `{k['name']}` (" + ', '.join('`' + c + '`' for c in cols) + ')')
            elif k['type'] == 'c':
                try:
                    lines.append(f"    CONSTRAINT `{unique_name(n, k['name'])}` CHECK ({convert_check(k['def'], n, k['name'])})")
                except Unsupported as exc:
                    report.append(f"{n}: CHECK {k['name']} not converted (needs a trigger or application validation): {str(exc)[:160]}")
            elif k['type'] not in ('f',):
                report.append(f"{n}: constraint {k['name']} ({k['type']}) {k['def']} not converted")
        constraint_idx = {k['name'] for k in t['cons'] or [] if k['type'] in ('p', 'u')}
        for ix in t['idx'] or []:
            if ix['primary'] or ix['name'] in constraint_idx:
                continue
            cols, where = split_index_columns(ix['def'])
            if where and ix['unique']:
                plain_cols = [c.strip('"') for c, _ in cols if re.fullmatch(r'"?\w+"?', c.strip())]
                if len(plain_cols) != len(cols):
                    raise SystemExit(f"partial unique index {ix['name']} on {n} has an expression column")
                lines.extend(partial_unique(n, ix['name'], plain_cols, where))
                report.append(f"{n}: partial unique index {ix['name']} ({where}) emulated by a generated column")
                continue
            if where:
                report.append(f"{n}: index {ix['name']} is partial ({where}); created as a plain index")
            rendered = []
            for c, desc in cols:
                c = c.strip()
                if re.fullmatch(r'"?\w+"?', c):
                    rendered.append('`' + c.strip('"') + '`' + (' DESC' if desc else ''))
                else:
                    c = re.sub(r'"(\w+)"', r'\1', c)
                    c = convert_expr(c, f"{n}.{ix['name']} index expression")
                    rendered.append('((' + c + '))')
            plain = [c.strip('"') for c, _ in cols if re.fullmatch(r'"?\w+"?', c.strip())]
            if len(plain) == len(cols) and key_bytes(plain, colinfo) > MAX_KEY_BYTES:
                if ix['unique']:
                    lines.extend(hashed_unique(n, ix['name'], plain, notnull, colinfo))
                    continue
                rendered = []
                for c, desc in cols:
                    c = c.strip('"')
                    prefix = f'({BIG_KEY})' if colinfo[c][0] in ('varchar', 'char', 'text') and (colinfo[c][1] or BIG_KEY) > BIG_KEY else ''
                    rendered.append(f'`{c}`{prefix}' + (' DESC' if desc else ''))
                report.append(f"{n}: index {ix['name']} is longer than 3072 bytes; string columns use a {BIG_KEY}-character prefix")
            kind = 'UNIQUE KEY' if ix['unique'] else 'KEY'
            lines.append(f"    {kind} `{ix['name']}` (" + ', '.join(rendered) + ')')
        stmts.append(f"CREATE TABLE IF NOT EXISTS `{n}` (\n" + ',\n'.join(lines) + '\n) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_bin')
        created.add(n)
        for k in t['cons'] or []:
            if k['type'] != 'f':
                continue
            m = re.match(r'FOREIGN KEY \((.*?)\) REFERENCES (?:public\.)?"?(\w+)"?\((.*?)\)(.*)$', k['def'])
            cols = ', '.join('`' + c.strip().strip('"') + '`' for c in m.group(1).split(','))
            refs = ', '.join('`' + c.strip().strip('"') + '`' for c in m.group(3).split(','))
            tail = m.group(4)
            actions = ''
            for kw in ('DELETE', 'UPDATE'):
                am = re.search(r'ON ' + kw + r' (CASCADE|SET NULL|RESTRICT|NO ACTION|SET DEFAULT)', tail)
                if am and am.group(1) not in ('NO ACTION',):
                    if am.group(1) == 'SET DEFAULT':
                        raise SystemExit(f'SET DEFAULT foreign key {k["name"]}')
                    actions += f' ON {kw} {am.group(1)}'
            counter += 1
            fk_name = unique_name(n, k['name'])
            fk_stmts.append((n, fk_name, f"ALTER TABLE `{n}` ADD CONSTRAINT `{fk_name}` FOREIGN KEY ({cols}) REFERENCES `{m.group(2)}` ({refs}){actions}"))
    out = [
        '-- Generated by scripts/pg2mysql_platform.py from the final PostgreSQL platform schema. Do not edit by hand.',
    ]
    out.extend(stmts)
    for i, (table, name, ddl) in enumerate(fk_stmts, 1):
        out.append(f"SET @tp_fk{i} := (SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = DATABASE() AND table_name = '{table}' AND constraint_name = '{name}')")
        out.append(f"SET @tp_fk{i}_sql := IF(@tp_fk{i} = 0, '{ddl}', 'SELECT 1')")
        out.append(f'PREPARE tp_fk{i}_stmt FROM @tp_fk{i}_sql')
        out.append(f'EXECUTE tp_fk{i}_stmt')
        out.append(f'DEALLOCATE PREPARE tp_fk{i}_stmt')
    sys.stdout.write('\n-- @@\n'.join(out) + '\n')
    for r in report:
        print(r, file=sys.stderr)


if __name__ == '__main__':
    main()
