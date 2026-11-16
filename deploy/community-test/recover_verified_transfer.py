"""Recover only verified transfer failures; source revision is derived from the run."""
import hashlib
import json
import os
import re
import pathlib
import subprocess
import tarfile

TRANSFER = 'Transfer verified bundles over artifact HTTPS'
DEPLOY_STEPS = ['Check coordinated runtime before replacing any service',
                'Deploy verified backend after all code checks pass', 'Upload source and deploy native services']
REQUIRED = ['Require main for runtime deployment', 'Verify migration history guard',
            'Verify durable command deadlines against PostgreSQL', 'Compile and test native services',
            'Verify offline conversion transaction against native schema',
            'Verify device automation queries against PostgreSQL', 'Verify scheduler transport',
            'Build complete native image away from the live server',
            'Package immutable tracked source and verified image', 'Preserve deployable native bundle',
            'Download already verified backend bundle', 'Build coordinated broker artifact',
            'Preserve coordinated broker bundle']


def validate_run(run, jobs, repo, source_run):
    if (run['id'] != source_run or not re.fullmatch('[0-9a-f]{40}', run['head_sha']) or run['head_branch'] != 'main'
            or run['event'] != 'workflow_dispatch' or run['path'] != '.github/workflows/deploy.yml'
            or run['repository']['full_name'] != repo or run['status'] != 'completed'
            or run['conclusion'] != 'failure'):
        raise ValueError('source run is not the approved transfer failure')
    backend = [j for j in jobs if j['name'] == 'build_backend']
    native = [j for j in jobs if j['name'] == 'native / deploy']
    if len(backend) != 1 or len(native) != 1 or backend[0]['conclusion'] != 'success':
        raise ValueError('verified backend job missing')
    job = native[0]
    if job['status'] != 'completed' or job['conclusion'] != 'failure':
        raise ValueError('native failure missing')
    steps = {s['name']: s for s in job['steps']}
    if any(steps.get(n, {}).get('conclusion') != 'success' for n in REQUIRED):
        raise ValueError('native build or check not successful')
    if [s['name'] for s in job['steps'] if s['conclusion'] == 'failure'] != [TRANSFER]:
        raise ValueError('failure is not exclusively artifact transfer')
    if any(steps.get(n, {}).get('conclusion') != 'skipped' for n in DEPLOY_STEPS):
        raise ValueError('source deployment already started')


def validate_changes(paths):
    if any(not (p.startswith('deploy/community-test/') or p.startswith('.github/workflows/')) for p in paths):
        raise ValueError('business source changed since verified build')


def checksum(stream):
    h = hashlib.sha256()
    for block in iter(lambda: stream.read(1024 * 1024), b''):
        h.update(block)
    return h.hexdigest()


def verify_tar(path, source_sha):
    with tarfile.open(path, 'r:gz') as archive:
        entries = archive.getmembers()
        names = [e.name for e in entries]
        if len(names) != len(set(names)) or any(e.issym() or e.islnk() or not (e.isfile() or e.isdir())
                or pathlib.PurePosixPath(e.name).is_absolute() or '..' in pathlib.PurePosixPath(e.name).parts for e in entries):
            raise ValueError('unsafe release archive')
        manifest = json.load(archive.extractfile('manifest.json'))
        actual = {e.name for e in entries if e.isfile() and e.name != 'manifest.json'}
        if manifest['commit'] != source_sha or actual != set(manifest['sha256']):
            raise ValueError('release manifest source/files mismatch')
        for name, expected in manifest['sha256'].items():
            if checksum(archive.extractfile(name)) != expected:
                raise ValueError('release content digest mismatch')


def api(path):
    return json.loads(subprocess.check_output(['gh', 'api', 'repos/' + os.environ['GITHUB_REPOSITORY'] + path]))


def main():
    if os.environ['GITHUB_REF'] != 'refs/heads/main':
        raise ValueError('recovery requires main')
    raw_run = os.environ.get('SOURCE_RUN_ID', '')
    if not re.fullmatch('[1-9][0-9]{0,19}', raw_run):
        raise ValueError('source run must be a positive integer')
    source_run = int(raw_run)
    repo = os.environ['GITHUB_REPOSITORY']
    run = api('/actions/runs/' + str(source_run))
    jobs = api('/actions/runs/' + str(source_run) + '/jobs?per_page=100')['jobs']
    validate_run(run, jobs, repo, source_run)
    source_sha = run['head_sha']
    subprocess.run(['git', 'merge-base', '--is-ancestor', source_sha, 'HEAD'], check=True)
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()
    if head != os.environ['GITHUB_SHA']:
        raise ValueError('checkout identity mismatch')
    changes = subprocess.check_output(['git', 'diff', '--name-only', '--no-renames', source_sha, 'HEAD'], text=True).splitlines()
    validate_changes(changes)
    artifacts = api('/actions/runs/' + str(source_run) + '/artifacts?per_page=100')['artifacts']
    selected = {}
    for kind, artifact in [('native', 'native-candidate-'), ('backend', 'backend-candidate-'), ('broker', 'broker-candidate-')]:
        matches = [a for a in artifacts if a['name'] == artifact + source_sha and not a['expired']]
        if len(matches) != 1:
            raise ValueError('exact source artifact missing')
        selected[kind] = matches[0]['id']
        subprocess.run(['gh', 'run', 'download', str(source_run), '--repo', repo,
                        '--name', artifact + source_sha, '--dir', '.'], check=True)
    verify_tar(pathlib.Path('encore-' + source_sha + '.tar.gz'), source_sha)
    verify_tar(pathlib.Path('backend-' + source_sha + '.tar.gz'), source_sha)
    broker = json.loads(pathlib.Path('broker-' + source_sha + '.json').read_text())
    with pathlib.Path('broker-' + source_sha + '.tar.gz').open('rb') as stream:
        if broker['commit'] != source_sha or broker['sha256'] != checksum(stream):
            raise ValueError('broker manifest mismatch')
    identity = {'source_run': source_run, 'build_commit': source_sha, 'deployment_helper_commit': head,
                'artifacts': selected}
    pathlib.Path('recovery-identity.json').write_text(json.dumps(identity, indent=2))
    print(json.dumps(identity))
    with open(os.environ['GITHUB_OUTPUT'], 'a') as stream:
        stream.write('build_commit=' + source_sha + '\n')
        for kind, artifact_id in selected.items():
            stream.write(kind + '_artifact_id=' + str(artifact_id) + '\n')


if __name__ == '__main__':
    main()
