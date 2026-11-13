"""Build-only source composition and acceptance gates for the staged Encore runtime.

Does not deploy, connect to production, mutate databases, or publish registries.
The unrelated current checkout supplies tooling only, never runtime source.
"""
import argparse
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import subprocess
import tarfile

BASE = 'ece5c7ef870acd54b712123831d615ed6e3858c2'
ISOLATION = 'ce9097abf20b13f7c7f072f64d435018801b926a'
ROOTS = ('encore-api', 'pkg/relational')
ISOLATION_FILES = {
    'encore-api/automation/a09_run_test.go',
    'encore-api/automation/ingress_auth.go',
    'encore-api/automation/q09_ingress_isolation_test.go',
    'encore-api/automation/service.go',
    'encore-api/automation/store/runtime_store.go',
}
OWNER_FILES = {
    'encore-api/thingmodel/core/command.go',
    'encore-api/thingmodel/core/command_test.go',
    'encore-api/thingmodel/owner_command_validation.go',
    'encore-api/thingmodel/owner_command_validation_test.go',
}
SERVICES = ['authn', 'thingmodel', 'devicemetadata', 'fileassets',
            'connectorcatalog', 'automation', 'notification', 'marketinstall', 'platformconfig']
REQUIRED_TESTS = {
    'TestQ09TrustedOwnerGatewayPathRetainsReceiptAndReplay',
    'TestQ09RawIngressSameSourceIDDoesNotCrossTenantDeduplication',
    'TestQ09ConcurrentTenantsMayReuseClientEventID',
    'TestQ09RawIngressRejectsForeignDeviceBeforeStateMutation',
    'TestQ09StoreRejectsForeignReceiptCollision',
    'TestQ09RawIngressKeepsOwnedLegacyReceiptIdempotent',
    'TestQ09PublicUplinkChecksDeviceAndGatewayOwnership',
    'TestQ09UserIngressFailsClosedWhenOwnershipCannotBeVerified',
    'TestEveryDeclaredStepKindHasARealImplementation',
    'TestOwnerCommandValidationIdentityPinsAndResultOnly',
    'TestPublishedCommandParameterContract',
    'TestPublishedCommandTypedEnumsAndExactNumber',
    'TestPostgresOwnerCommandValidationUsesCommunityTenantMapping',
}

def git(repo, *args):
    # Git archive may apply working-tree EOL attributes; source identity must
    # use identical repository bytes on Windows and the Linux builder.
    return subprocess.check_output(['git', '-c', 'core.autocrlf=false', *args], cwd=repo)

def digest(data):
    return hashlib.sha256(data).hexdigest()

def commit(repo, value):
    if not re.fullmatch('[0-9a-f]{40}', value):
        raise ValueError('source must be a full immutable commit SHA')
    if git(repo, 'rev-parse', value + '^{commit}').decode().strip() != value:
        raise ValueError('source is not the requested commit')
    return value

def archive_files(repo, revision):
    result = {}
    with tarfile.open(fileobj=io.BytesIO(git(repo, 'archive', '--format=tar', revision, *ROOTS))) as archive:
        for item in archive:
            name = PurePosixPath(item.name)
            if name.is_absolute() or '..' in name.parts:
                raise ValueError('unsafe archive path')
            if item.isdir():
                continue
            if not item.isfile():
                raise ValueError('candidate source must contain regular files only')
            result[item.name] = archive.extractfile(item).read()
    return result

def changed_paths(repo, revision, *paths):
    return set(git(repo, 'diff-tree', '--no-commit-id', '--name-only', '-r', revision, '--', *paths).decode().splitlines())

def compose(repo, owner, dest):
    repo, dest = Path(repo), Path(dest)
    for revision in (BASE, ISOLATION, owner):
        commit(repo, revision)
    if dest.exists():
        raise ValueError('refusing to overwrite an existing candidate')
    source = archive_files(repo, BASE)
    baseline = dict(source)
    if changed_paths(repo, ISOLATION) != ISOLATION_FILES:
        raise ValueError('isolation patch scope changed')
    for name in sorted(ISOLATION_FILES):
        if name in source and source[name] != git(repo, 'show', ISOLATION + '^:' + name):
            raise ValueError('isolation preimage differs from deployed base: ' + name)
        source[name] = git(repo, 'show', ISOLATION + ':' + name)
    # The owner commit may also contain Gin code, but only these four additive
    # Encore files can enter this candidate. No module/migration dependency drift.
    if changed_paths(repo, owner, *ROOTS) != OWNER_FILES:
        raise ValueError('owner runtime dependency set is not the reviewed four files')
    for name in sorted(OWNER_FILES):
        if name in source:
            raise ValueError('owner endpoint expected to be purely additive: ' + name)
        source[name] = git(repo, 'show', owner + ':' + name)
    migrations = {n: digest(b) for n, b in baseline.items() if '/migrations/' in n and n.endswith('.sql')}
    if len(migrations) != 61 or any(digest(source[n]) != sha for n, sha in migrations.items()):
        raise ValueError('deployed migration set must remain exactly unchanged')
    expected = ISOLATION_FILES | OWNER_FILES
    if {n for n in source if n not in baseline or source[n] != baseline[n]} != expected:
        raise ValueError('unexpected composed source differences')
    manifest = {
        'schema_version': 1, 'base_commit': BASE, 'isolation_commit': ISOLATION,
        'owner_commit': owner, 'tooling_commit': git(repo, 'rev-parse', 'HEAD').decode().strip(),
        'services': SERVICES, 'migrations': migrations,
        'changed_files': sorted(expected), 'source_sha256': {n: digest(b) for n, b in sorted(source.items())},
        'deployment_performed': False,
    }
    manifest['composition_sha256'] = digest(json.dumps(manifest['source_sha256'], sort_keys=True).encode())
    for name, data in source.items():
        target = dest / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
    (dest / 'composition.json').write_text(json.dumps(manifest, indent=2), encoding='utf-8')
    return manifest

def test_gate(paths):
    passed, failures, skipped = set(), [], []
    for path in paths:
        for line in Path(path).read_text(encoding='utf-8-sig').splitlines():
            try:
                row = json.loads(line)
            except ValueError:
                continue
            action, name = row.get('Action'), row.get('Test', '')
            if action == 'fail':
                failures.append(name or row.get('Package'))
            if action == 'skip':
                skipped.append(name or row.get('Package'))
            if action == 'pass' and name and '/' not in name:
                passed.add(name)
    missing = sorted(REQUIRED_TESTS - passed)
    report = {'passed_tests': sorted(passed), 'missing': missing, 'failed': failures, 'skipped': skipped}
    if missing or failures or skipped:
        raise ValueError('candidate test gate failed: ' + json.dumps(report))
    return report

def verify_source(dest):
    dest = Path(dest)
    manifest = json.loads((dest / 'composition.json').read_text())
    for name, expected in manifest['source_sha256'].items():
        if digest((dest / name).read_bytes()) != expected:
            raise ValueError('candidate source modified after composition: ' + name)
    actual = {str(p.relative_to(dest)).replace('\\', '/') for root in ROOTS
              for p in (dest / root).rglob('*') if p.is_file()
              and '.encore' not in p.relative_to(dest).parts}
    if actual != set(manifest['source_sha256']):
        raise ValueError('untracked source added after composition')
    return manifest

def main():
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest='mode', required=True)
    c = sub.add_parser('compose'); c.add_argument('--repo', default='.'); c.add_argument('--owner-commit', required=True); c.add_argument('--dest', required=True)
    g = sub.add_parser('test-gate'); g.add_argument('logs', nargs='+'); g.add_argument('--report', required=True)
    v = sub.add_parser('verify-source'); v.add_argument('--dest', required=True)
    args = p.parse_args()
    if args.mode == 'compose':
        manifest = compose(args.repo, args.owner_commit, args.dest)
        print(json.dumps({'composition_sha256': manifest['composition_sha256'], 'changed_files': len(manifest['changed_files']), 'migrations': len(manifest['migrations'])}))
    elif args.mode == 'test-gate':
        result = test_gate(args.logs)
        Path(args.report).write_text(json.dumps(result, indent=2), encoding='utf-8')
        print('Required real database/contract tests passed, none skipped')
    else:
        verify_source(args.dest); print('Composed source identity verified')

if __name__ == '__main__':
    main()
