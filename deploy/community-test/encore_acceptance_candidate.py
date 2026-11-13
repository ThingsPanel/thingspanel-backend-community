"""Build-only composition of reviewed acceptance fixes onto the deployed Encore tree.

The checkout supplies tooling, never runtime source. No deployment or registry write.
"""
import argparse
import importlib.util
import json
from pathlib import Path, PurePosixPath
import re
import tempfile

HERE = Path(__file__).resolve().parent
module_spec = importlib.util.spec_from_file_location('bootstrap', HERE / 'encore_isolation_candidate.py')
bootstrap = importlib.util.module_from_spec(module_spec)
module_spec.loader.exec_module(bootstrap)
SPEC_PATH = HERE / 'encore_acceptance_overlays.json'


def specification():
    spec = json.loads(SPEC_PATH.read_text(encoding='utf-8'))
    if spec['schema_version'] != 1 or len(spec['overlays']) != 5:
        raise ValueError('unreviewed overlay specification')
    if spec['bootstrap_owner_commit'] != '47405bfd51eba9f436231744de9a123d4278152d':
        raise ValueError('bootstrap command owner must remain the original four-file patch')
    for overlay in spec['overlays']:
        for name, hashes in overlay['files'].items():
            path = PurePosixPath(name)
            if path.is_absolute() or '..' in path.parts or not name.startswith('encore-api/'):
                raise ValueError('overlay path outside reviewed Encore scope')
            if '/migrations/' in name or name.endswith(('/go.mod', '/go.sum', '/encore.app')):
                raise ValueError('migration or module dependency changes are prohibited')
            for key in ('before', 'after'):
                if hashes[key] is not None and not re.fullmatch('[0-9a-f]{64}', hashes[key]):
                    raise ValueError('invalid source hash')
    return spec


def source_commits(spec):
    return sorted({bootstrap.BASE, bootstrap.ISOLATION, spec['bootstrap_owner_commit'], spec['bootstrap_anchor']}
                  | {o[k] for o in spec['overlays'] for k in ('base', 'source')})


def apply_overlay(repo, source, overlay):
    for revision in (overlay['base'], overlay['source']):
        bootstrap.commit(repo, revision)
    changed = set(bootstrap.git(repo, 'diff', '--name-only', overlay['base'], overlay['source'], '--', *bootstrap.ROOTS).decode().splitlines())
    if changed != set(overlay['files']):
        raise ValueError('unreviewed runtime dependency set: ' + overlay['name'])
    existing = set(bootstrap.git(repo, 'ls-tree', '-r', '--name-only', overlay['base']).decode().splitlines())
    updates = {}
    for name, expected in sorted(overlay['files'].items()):
        old = bootstrap.git(repo, 'show', overlay['base'] + ':' + name) if name in existing else None
        if source.get(name) != old or (bootstrap.digest(old) if old is not None else None) != expected['before']:
            raise ValueError('overlay preimage mismatch: ' + name)
        new = bootstrap.git(repo, 'show', overlay['source'] + ':' + name)
        if bootstrap.digest(new) != expected['after']:
            raise ValueError('overlay postimage mismatch: ' + name)
        updates[name] = new
    source.update(updates)


def compose(repo, dest):
    repo, dest = Path(repo).resolve(), Path(dest).resolve()
    if dest.exists() or dest == repo or repo in dest.parents:
        raise ValueError('candidate destination must be new and outside tooling checkout')
    spec = specification()
    for revision in source_commits(spec):
        bootstrap.commit(repo, revision)
    # Reuse the previously reviewed ece5 + ce9097 + original 474 composition.
    with tempfile.TemporaryDirectory(prefix='tp-encore-bootstrap-') as temp:
        initial = Path(temp) / 'source'
        manifest = bootstrap.compose(repo, spec['bootstrap_owner_commit'], initial)
        source = {name: (initial / name).read_bytes() for name in manifest['source_sha256']}
    if source != bootstrap.archive_files(repo, spec['bootstrap_anchor']):
        raise ValueError('bootstrap differs from the deployed runtime composition anchor')
    baseline = dict(source)
    for overlay in spec['overlays']:
        apply_overlay(repo, source, overlay)
    migrations = {n: bootstrap.digest(v) for n, v in source.items() if '/migrations/' in n and n.endswith('.sql')}
    if migrations != manifest['migrations'] or len(migrations) != 61:
        raise ValueError('deployed migration set changed')
    expected = set().union(*(set(o['files']) for o in spec['overlays']))
    changed = {n for n in source if source[n] != baseline.get(n)}
    if changed != expected or len(changed) != 29:
        raise ValueError('combined delta must contain exactly the reviewed 29 files')
    # Ensure the frozen gate names still belong to the intended package source.
    actual_tests = {}
    for name, data in source.items():
        if not name.endswith('_test.go'):
            continue
        package = 'encore.app/' + str(PurePosixPath(name).parent).removeprefix('encore-api/')
        for test in re.findall(rb'^func (Test\w+)\(', data, re.M):
            actual_tests.setdefault(test.decode(), set()).add(package)
    if any(package not in actual_tests.get(name, set()) for name, package in spec['required_tests'].items()):
        raise ValueError('required test missing from composed package')
    manifest.update({
        'schema_version': 2, 'bootstrap_anchor': spec['bootstrap_anchor'],
        'bootstrap_composition_sha256': manifest['composition_sha256'],
        'overlays': spec['overlays'], 'overlay_spec_sha256': bootstrap.digest(SPEC_PATH.read_bytes()),
        'delta_files': sorted(changed), 'required_tests': spec['required_tests'],
        'source_sha256': {n: bootstrap.digest(v) for n, v in sorted(source.items())},
        'release_order': ['Gin additive exact-device directory', 'Encore owner and automation', 'Gin mandatory uplink validation'],
    })
    deployed_base = bootstrap.archive_files(repo, bootstrap.BASE)
    manifest['changed_files'] = sorted(n for n in source if source[n] != deployed_base.get(n))
    manifest['composition_sha256'] = bootstrap.digest(json.dumps(manifest['source_sha256'], sort_keys=True).encode())
    if manifest['composition_sha256'] != spec['expected_composition_sha256']:
        raise ValueError('composed runtime differs from the reviewed complete source fingerprint')
    for name, data in source.items():
        target = dest / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
    (dest / 'composition.json').write_text(json.dumps(manifest, indent=2), encoding='utf-8')
    return manifest


def test_gate(paths):
    spec = specification()
    passed, complete, failures, skips = set(), set(), [], []
    empty, package_skips, packages_with_tests = set(), set(), set()
    for path in paths:
        for line in Path(path).read_text(encoding='utf-8-sig').splitlines():
            try:
                row = json.loads(line)
            except ValueError:
                continue
            action, name, package = row.get('Action'), row.get('Test', ''), row.get('Package', '')
            if name:
                packages_with_tests.add(package)
            if action == 'fail':
                failures.append(name or package)
            if action == 'skip':
                (skips.append(name) if name else package_skips.add(package))
            if action == 'output' and not name and any(m in row.get('Output', '') for m in ('[no test files]', '[no tests to run]')):
                empty.add(package)
            if action == 'pass':
                if name and '/' not in name:
                    passed.add((name, package))
                elif not name:
                    complete.add(package)
    missing = sorted(n for n, pkg in spec['required_tests'].items() if (n, pkg) not in passed)
    incomplete = sorted(set(spec['required_tests'].values()) - complete)
    allowed_empty = (package_skips & empty) - packages_with_tests
    skips.extend(sorted(package_skips - allowed_empty))
    report = {'required_test_count': len(spec['required_tests']), 'passed_tests': sorted(n for n, _ in passed),
              'missing': missing, 'incomplete_packages': incomplete, 'failed': failures, 'skipped': skips,
              'packages_without_matching_tests': sorted(allowed_empty)}
    if missing or incomplete or failures or skips:
        raise ValueError('real PG acceptance gate failed: ' + json.dumps(report))
    return report


def verify_source(dest):
    manifest = bootstrap.verify_source(dest)
    spec = specification()
    if manifest.get('services') != bootstrap.SERVICES or len(manifest.get('migrations', {})) != 61:
        raise ValueError('service or migration manifest changed')
    if manifest.get('overlays') != spec['overlays'] or manifest.get('required_tests') != spec['required_tests']:
        raise ValueError('reviewed overlay or test manifest changed')
    if manifest.get('overlay_spec_sha256') != bootstrap.digest(SPEC_PATH.read_bytes()):
        raise ValueError('overlay specification changed')
    if manifest.get('composition_sha256') != bootstrap.digest(json.dumps(manifest['source_sha256'], sort_keys=True).encode()):
        raise ValueError('composition fingerprint changed')
    if manifest.get('composition_sha256') != spec['expected_composition_sha256']:
        raise ValueError('source and manifest no longer match the reviewed runtime')
    migrations = {n: sha for n, sha in manifest['source_sha256'].items() if '/migrations/' in n and n.endswith('.sql')}
    if manifest['migrations'] != migrations:
        raise ValueError('migration metadata differs from verified source')
    return manifest


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest='mode', required=True)
    command = sub.add_parser('compose'); command.add_argument('--repo', default='.'); command.add_argument('--dest', required=True)
    gate = sub.add_parser('test-gate'); gate.add_argument('logs', nargs='+'); gate.add_argument('--report', required=True)
    verify = sub.add_parser('verify-source'); verify.add_argument('--dest', required=True)
    sub.add_parser('source-commits'); sub.add_parser('test-regex')
    args = parser.parse_args()
    if args.mode == 'compose':
        m = compose(args.repo, args.dest)
        print(json.dumps({'composition_sha256': m['composition_sha256'], 'delta_files': len(m['delta_files']), 'migrations': len(m['migrations']), 'services': len(m['services'])}))
    elif args.mode == 'test-gate':
        report = test_gate(args.logs)
        Path(args.report).write_text(json.dumps(report, indent=2), encoding='utf-8')
        print('All required real PG/contract tests passed; zero skipped')
    elif args.mode == 'verify-source':
        verify_source(args.dest); print('Reviewed composed source identity verified')
    elif args.mode == 'source-commits':
        print('\n'.join(source_commits(specification())))
    else:
        print('^(' + '|'.join(re.escape(n) for n in specification()['required_tests']) + ')$')


if __name__ == '__main__':
    main()
