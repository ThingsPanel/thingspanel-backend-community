"""Compose the deployed Gin source plus the reviewed bounded directory increment, never an owner merge.

This is a build-only tool: it has no SSH, upload-to-server, restart or migration
operation. The command-validation commit's whole stage anchor is never merged.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

BASE_SOURCE = '1af192946db407e05909827ce627f1f82755640e'
CAS_SOURCE = '05a6c75e99afbcc01987f0f219ac7704a9bfa01a'
COMMAND_SOURCE = '47405bfd51eba9f436231744de9a123d4278152d'
COMMAND_PARENT = '54209fba3a9cd2e2f5ac8d96cc253276b299f9a3'
DIRECTORY_SOURCE = 'c4197017533974a624a3fa0982a0f79cd6443ec7'
DIRECTORY_PARENT = '9766c01000960cd8ef66a9f1eb2a0bfe2a02328f'
DEPLOYED_TOOL_SOURCE = '1702cc2daf9267d590c9d3e0f41fb56cdc02b4bd'
DEPLOYED_SOURCE_SNAPSHOT_SHA256 = 'a6e278f62d51c881d5a9b3a46e314a1994d5f01b5ee6eadc36486aeef5d8b8b4'
DIRECTORY_FILES = (
 'internal/api/automation_owner.go',
 'internal/api/automation_device_lookup.go',
 'internal/api/automation_device_lookup_test.go',
)
DIRECTORY_DOCS = ('docs/WORKSPACES.md', 'docs/tasks/Q09-BOUNDED-DIRECTORY-OWNER.md')
REQUIRED_DIRECTORY_TESTS = (
 'TestAutomationDeviceLookupPostgresIsBoundedAndTenantScoped',
 'TestAutomationDeviceLookupRejectsInvalidRequestsBeforeQuery',
 'TestAutomationOwnerAuthRunsBeforeDeviceOrCommandProcessing',
)
COMMAND_FILES = (
 'internal/api/command_set_log.go',
 'internal/brokersemantic/command.go',
 'internal/service/command_data.go',
 'internal/service/command_durable.go',
 'internal/service/command_expected.go',
 'internal/service/expected_data.go',
 'internal/service/expected_data_ingress.go',
 'internal/service/native_command_owner_validation.go',
 'internal/service/native_command_owner_validation_test.go',
 'internal/service/native_command_validation.go',
 'internal/service/native_command_validation_test.go',
)
EXCLUDED_ENCORE_FILES = (
 'encore-api/thingmodel/core/command.go',
 'encore-api/thingmodel/core/command_test.go',
 'encore-api/thingmodel/owner_command_validation.go',
 'encore-api/thingmodel/owner_command_validation_test.go',
)
CAS_INPUTS = (
 'deploy/community-test/apply_template_metadata_cas.py',
 'deploy/community-test/template_metadata_stage_owner.go.txt',
 'internal/api/device_config.go', 'internal/service/device_config.go',
 'internal/model/device_config_version.go', 'internal/model/device_config_version_test.go',
 'internal/dal/device_config_metadata.go', 'internal/dal/device_config_metadata_postgres_test.go',
 'internal/api/device_config_metadata_test.go',
)
REQUIRED_PG_TESTS = (
 'TestDeviceConfigMetadataPostgres',
 'TestTemplateMetadataAPI',
 'TestPostgresNativeCommandOwnerValidationAllPreparationPaths',
    'TestPostgresPluginPublisherAuthenticatesWhileDispatchHoldsRoute',
) + REQUIRED_DIRECTORY_TESTS


def run(args, cwd=None, data=None):
    # Historical composers spawn Git from exported directories. Pin Git's
    # newline policy for their child processes as well as our checkout.
    env = os.environ.copy()
    index = int(env.get('GIT_CONFIG_COUNT', '0'))
    env['GIT_CONFIG_COUNT'] = str(index + 1)
    env['GIT_CONFIG_KEY_' + str(index)] = 'core.autocrlf'
    env['GIT_CONFIG_VALUE_' + str(index)] = 'false'
    result = subprocess.run([str(x) for x in args], cwd=cwd, input=data,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
    if result.returncode:
        raise RuntimeError('Candidate operation failed: ' + str(args[0]) + ': ' + result.stderr.decode(errors='replace')[-2000:])
    return result.stdout


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def snapshot(root):
    root = Path(root)
    result = {}
    for path in sorted(root.rglob('*')):
        if '.git' in path.relative_to(root).parts:
            continue
        if path.is_symlink():
            raise ValueError('Source tree contains a symlink')
        if path.is_file():
            result[path.relative_to(root).as_posix()] = digest(path.read_bytes())
    return result


def verify_only_changes(before, after, allowed):
    changed = {name for name in set(before) | set(after) if before.get(name) != after.get(name)}
    if changed != set(allowed):
        raise ValueError('Unexpected composed source delta: ' + str(sorted(changed ^ set(allowed))))
    if any(name.startswith(('encore-api/', 'notification-adapters/', 'sql/', 'migrations/')) for name in changed):
        raise ValueError('Gin candidate must not change another owner or database schema')
    return [{'path': name, 'before_sha256': before.get(name), 'after_sha256': after.get(name)} for name in sorted(changed)]


def verify_deployed_source(files):
    current = digest(json.dumps(files, sort_keys=True, separators=(',', ':')).encode())
    if current != DEPLOYED_SOURCE_SNAPSHOT_SHA256:
        raise ValueError('Reconstructed Gin source differs from deployed 1702 manifest')


def apply_directory_files(backend, entries):
    backend = Path(backend)
    if set(entries) != set(DIRECTORY_FILES):
        raise ValueError('Directory delta must contain exactly the three reviewed files')
    # Check all preimages before changing any source file.
    for name, (before, after) in entries.items():
        target = backend / name
        actual = target.read_bytes() if target.exists() else None
        if actual != before:
            raise ValueError('Directory baseline mismatch: ' + name)
    changes = []
    for name, (before, after) in entries.items():
        target = backend / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(after)
        changes.append({'path': name, 'before_sha256': digest(before) if before is not None else None,
                        'after_sha256': digest(after)})
    return changes


def load_module(path):
    spec = importlib.util.spec_from_file_location(path.stem, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def compose(repo, destination):
    repo, destination = Path(repo).resolve(), Path(destination).resolve()
    if destination.exists() or repo == destination or repo in destination.parents:
        raise ValueError('Output must be a new directory outside the source worktree')
    def git(*args):
        return run(['git', *args], repo)
    head = git('rev-parse', 'HEAD').decode().strip()
    for sha in (BASE_SOURCE, CAS_SOURCE, COMMAND_SOURCE, COMMAND_PARENT, DIRECTORY_SOURCE, DIRECTORY_PARENT, DEPLOYED_TOOL_SOURCE):
        git('cat-file', '-e', sha + '^{commit}')
    if git('rev-parse', COMMAND_SOURCE + '^').decode().strip() != COMMAND_PARENT:
        raise ValueError('Command candidate parent changed')
    changed_command = set(git('diff', '--name-only', COMMAND_PARENT, COMMAND_SOURCE).decode().splitlines())
    if changed_command != set(COMMAND_FILES) | set(EXCLUDED_ENCORE_FILES):
        raise ValueError('Command commit exceeds the reviewed 11 Gin + 4 Encore files')
    git('merge-base', '--is-ancestor', CAS_SOURCE, head)
    git('merge-base', '--is-ancestor', DEPLOYED_TOOL_SOURCE, head)
    if git('rev-parse', DIRECTORY_SOURCE + '^').decode().strip() != DIRECTORY_PARENT:
        raise ValueError('Directory source parent changed')
    changed_directory = set(git('diff', '--name-only', DIRECTORY_PARENT, DIRECTORY_SOURCE).decode().splitlines())
    if changed_directory != set(DIRECTORY_FILES) | set(DIRECTORY_DOCS):
        raise ValueError('Directory source exceeds the reviewed files')
    # A real isolated checkout makes the historical composer see exactly 1af.
    # Neither another worktree's HEAD nor its dirty files can affect this build.
    destination.mkdir(parents=True)
    baseline_repo = destination / '_baseline_checkout'
    run(['git', 'clone', '--shared', '--no-checkout', repo, baseline_repo])
    run(['git', 'config', 'core.autocrlf', 'false'], baseline_repo)
    run(['git', 'checkout', '--detach', BASE_SOURCE], baseline_repo)
    if run(['git', 'status', '--porcelain'], baseline_repo).strip():
        raise ValueError('Historical checkout is not clean')
    baseline_tool = baseline_repo / 'deploy/community-test/mqtt_publisher_release.py'
    composed = destination / 'source'
    run([sys.executable, baseline_tool, 'sources', composed], baseline_repo)
    baseline_profile = json.loads((composed / 'RELEASE_PROFILE.json').read_text())
    if baseline_profile['source_commit'] != BASE_SOURCE:
        raise ValueError('Historical composition did not use 1af')
    backend = composed / 'backend'
    before = snapshot(backend)
    broker_before = snapshot(composed / 'broker')

    cas_root = destination / '_cas_inputs'
    cas_inputs = {}
    for name in CAS_INPUTS:
        raw = git('show', CAS_SOURCE + ':' + name)
        target = cas_root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(raw)
        cas_inputs[name] = digest(raw)
    cas_tool = load_module(cas_root / 'deploy/community-test/apply_template_metadata_cas.py')
    cas = cas_tool.apply(cas_root, backend)
    command_inputs = []
    for name in COMMAND_FILES:
        present = bool(git('ls-tree', COMMAND_PARENT, '--', name).strip())
        expected_before = digest(git('show', COMMAND_PARENT + ':' + name)) if present else None
        actual_before = digest((backend / name).read_bytes()) if (backend / name).exists() else None
        if actual_before != expected_before:
            raise ValueError('Command baseline mismatch: ' + name)
        expected_after = digest(git('show', COMMAND_SOURCE + ':' + name))
        command_inputs.append({'path': name, 'before_sha256': expected_before, 'after_sha256': expected_after})
    patch = git('diff', '--binary', COMMAND_PARENT, COMMAND_SOURCE, '--', *COMMAND_FILES)
    run(['git', '-c', 'core.autocrlf=false', 'apply', '--check'], backend, patch)
    run(['git', '-c', 'core.autocrlf=false', 'apply'], backend, patch)
    for item in command_inputs:
        if digest((backend / item['path']).read_bytes()) != item['after_sha256']:
            raise ValueError('Command result mismatch: ' + item['path'])
    deployed_snapshot = snapshot(backend)
    verify_deployed_source(deployed_snapshot)
    directory_entries = {}
    for name in DIRECTORY_FILES:
        present = bool(git('ls-tree', DIRECTORY_PARENT, '--', name).strip())
        directory_entries[name] = (git('show', DIRECTORY_PARENT + ':' + name) if present else None,
                                   git('show', DIRECTORY_SOURCE + ':' + name))
    directory_changes = apply_directory_files(backend, directory_entries)
    after = snapshot(backend)
    verify_only_changes(deployed_snapshot, after, DIRECTORY_FILES)
    allowed = set(COMMAND_FILES) | {entry['path'] for entry in cas['changes']} | set(DIRECTORY_FILES)
    changes = verify_only_changes(before, after, allowed)
    if snapshot(composed / 'broker') != broker_before:
        raise ValueError('Broker source changed during Gin-only composition')
    profile = {
        'profile': 'gin-template-cas-command-validation-bounded-directory', 'candidate_commit': head,
        'base_composition_commit': BASE_SOURCE, 'cas_source_commit': CAS_SOURCE,
        'command_source_commit': COMMAND_SOURCE, 'command_parent_anchor': COMMAND_PARENT,
        'command_anchor_merged': False, 'cas_input_sha256': cas_inputs,
        'baseline_composition': baseline_profile, 'cas_transform': cas,
        'command_delta_sha256': digest(patch), 'command_changes': command_inputs,
        'directory_source_commit': DIRECTORY_SOURCE, 'directory_parent_anchor': DIRECTORY_PARENT,
        'directory_anchor_merged': False, 'directory_changes': directory_changes,
        'deployed_tool_source': DEPLOYED_TOOL_SOURCE,
        'deployed_source_snapshot_sha256': DEPLOYED_SOURCE_SNAPSHOT_SHA256,
        'directory_tests': {'real_postgres': REQUIRED_DIRECTORY_TESTS[0], 'preconditions': REQUIRED_DIRECTORY_TESTS[1:]},
        'changes': changes, 'unchanged_file_count': len(set(before) & set(after)) - sum(x['before_sha256'] is not None for x in changes),
        'unchanged_broker_file_count': len(broker_before),
        'encore_changed': False, 'broker_changed': False, 'database_owner_changed': False,
        'required_encore_contract': 'authenticated POST /internal/thingmodel/validate-command',
        'required_encore_source_files': {name: digest(git('show', COMMAND_SOURCE + ':' + name)) for name in EXCLUDED_ENCORE_FILES},
        'deployment_order': ['Existing Encore validator contract remains unchanged', 'Additive Gin bounded-directory candidate', 'Bounded-directory API preflight', 'New Encore consumer only after owner endpoint verified'],
        'rollback_order': ['Gin before removing Encore validator contract'],
    }
    (destination / 'SOURCE_PROVENANCE.json').write_text(json.dumps(profile, indent=2), encoding='utf-8')
    (destination / 'SOURCE_FILES.json').write_text(json.dumps(after, indent=2), encoding='utf-8')
    return profile


def verify_tests(path, required=REQUIRED_PG_TESTS):
    rows = [json.loads(line) for line in Path(path).read_text().splitlines() if line.strip()]
    for name in required:
        terminal = [row['Action'] for row in rows if row.get('Test') == name and row.get('Action') in ('pass', 'fail', 'skip')]
        if terminal != ['pass']:
            raise ValueError('Required test missing, skipped or failed: ' + name)
    if any(row.get('Test') and row.get('Action') == 'skip' for row in rows):
        raise ValueError('Candidate test log contains a skipped test')
    if any(row.get('Action') == 'fail' for row in rows):
        raise ValueError('Candidate test log contains failures')


def package(source, binary, output, commit):
    import tarfile
    source, binary, output = Path(source), Path(binary), Path(output)
    profile = json.loads((source / 'SOURCE_PROVENANCE.json').read_text())
    if profile['candidate_commit'] != commit:
        raise ValueError('Binary candidate commit differs from source provenance')
    if snapshot(source / 'source/backend') != json.loads((source / 'SOURCE_FILES.json').read_text()):
        raise ValueError('Composed source changed after provenance generation')
    expected = 'gin-cas-command-' + commit
    if run([binary.resolve(), '--version']).decode().strip() != expected:
        raise ValueError('Built binary version differs from candidate commit')
    if binary.read_bytes()[:4] != b'\x7fELF':
        raise ValueError('Candidate must be a Linux ELF binary')
    files = {'ThingsPanel-Go': binary, 'SOURCE_PROVENANCE.json': source / 'SOURCE_PROVENANCE.json', 'SOURCE_FILES.json': source / 'SOURCE_FILES.json'}
    manifest = {'profile': profile['profile'], 'commit': commit, 'backend_version': expected,
                'sha256': {name: digest(path.read_bytes()) for name, path in files.items()},
                'encore_changed': False, 'broker_changed': False, 'migration_required': False,
                'deployment_precondition': profile['required_encore_contract']}
    manifest_path = source / 'manifest.json'
    manifest_path.write_text(json.dumps(manifest, indent=2), encoding='utf-8')
    files['manifest.json'] = manifest_path
    with tarfile.open(output, 'w:gz') as archive:
        for name, path in sorted(files.items()):
            archive.add(path, arcname=name)
    return {'archive': output.name, 'archive_sha256': digest(output.read_bytes()), 'manifest': manifest}


if __name__ == '__main__':
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest='operation', required=True)
    c = sub.add_parser('compose'); c.add_argument('destination'); c.add_argument('--repo', default=str(Path(__file__).resolve().parents[2]))
    t = sub.add_parser('verify-tests'); t.add_argument('log')
    b = sub.add_parser('package'); b.add_argument('source'); b.add_argument('binary'); b.add_argument('output'); b.add_argument('commit')
    args = p.parse_args()
    if args.operation == 'compose':
        result = compose(args.repo, args.destination)
        print(json.dumps({'candidate_commit': result['candidate_commit'], 'changed_files': len(result['changes']), 'unchanged_files': result['unchanged_file_count']}))
    elif args.operation == 'verify-tests':
        verify_tests(args.log); print('Required real PostgreSQL tests passed without skips')
    else:
        print(json.dumps(package(args.source, args.binary, args.output, args.commit)))
