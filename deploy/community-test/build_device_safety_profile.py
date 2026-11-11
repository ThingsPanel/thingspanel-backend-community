"""Reproducible interim security bundle; does not switch device/schema owners."""
import hashlib
import io
import json
import pathlib
import subprocess
import sys
import tarfile

BASELINE = '70bd7a1d31c11a02f3b32a106b0290ebe722b580'
PATCHES = (
    'a89145dc37a69521e5b09cc4c75bce6f688a849c',
    '006d41c890294a23d7edd764578b2e4f5a08107f',
    '6c244d7382993473be3268a4c41117471dddce6e',
    'bcdc25e7433e03b204f30ec938fbb1d5ac2c2013',
    '2503e1d8a61b224cb70196f50f8d84dfca30d0aa',
    '547482f6d9d91d186e2e6aba497c1af210737d7f',
)
FIXTURE = 'internal/api/telemetry_http_auth_test.go'


def build(repo, target):
    repo, target = pathlib.Path(repo).resolve(), pathlib.Path(target).resolve()
    if target.exists():
        raise ValueError('Refusing to overwrite an existing source directory')
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=repo)
    head = git('rev-parse', 'HEAD').decode().strip()
    for sha in (BASELINE, *PATCHES):
        subprocess.run(['git', 'merge-base', '--is-ancestor', sha, head], cwd=repo, check=True)
    target.mkdir(parents=True)
    with tarfile.open(fileobj=io.BytesIO(git('archive', BASELINE))) as archive:
        archive.extractall(target, filter='data')
    changed = set()
    for sha in PATCHES:
        paths = git('diff', '--name-only', sha + '^', sha, '--', 'internal', 'pkg').decode().splitlines()
        # A safety profile must not acquire a new database or runtime owner.
        if any(not p.startswith(('internal/api/', 'internal/service/', 'internal/dal/', 'internal/middleware/', 'pkg/global/')) for p in paths):
            raise ValueError('Patch exceeds the security profile scope')
        patch = git('diff', sha + '^', sha, '--', 'internal', 'pkg')
        subprocess.run(['git', 'apply', '--check'], cwd=target, input=patch, check=True)
        subprocess.run(['git', 'apply'], cwd=target, input=patch, check=True)
        changed.update(paths)
    # This test fixture includes the primary-store API key required by the fix.
    (target / FIXTURE).write_bytes(git('show', PATCHES[-1] + ':' + FIXTURE))
    changed.add(FIXTURE)
    profile = {'profile': 'device-safety', 'source_commit': head, 'baseline': BASELINE,
               'patches': list(PATCHES), 'fixture_source': PATCHES[-1],
               'changed_files': {p: hashlib.sha256((target / p).read_bytes()).hexdigest() for p in sorted(changed)},
               'database_owner_changed': False, 'device_implementation_changed': False}
    (target / 'RELEASE_PROFILE.json').write_text(json.dumps(profile, indent=2), encoding='utf8')
    return profile


if __name__ == '__main__':
    print(json.dumps(build(pathlib.Path(__file__).resolve().parents[2], sys.argv[1])))
