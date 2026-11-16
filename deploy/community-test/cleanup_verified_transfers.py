"""Remove only explicitly approved redundant transfer files, preserving releases.

Executed with inspect_runtime.py definitions bundled before this module so the
same read-only archive/release verification used for approval is reused.
"""
import base64
import sys


def approved_targets(plan, verified, active):
    rows = plan.get('files', [])
    if not 1 <= len(rows) <= 100 or len({r['package'] for r in rows}) != len(rows):
        raise RuntimeError('invalid approval manifest')
    available = {r['package']: r for r in verified if r.get('verified_copy')}
    targets = []
    for row in rows:
        path = pathlib.Path(row['package'])
        match = re.fullmatch(r'(backend|encore)-([0-9a-f]{40})\.tar\.gz', path.name)
        if not match or path.parent != pathlib.Path('/opt/tp-community-actions/incoming') or match.group(2) in active:
            raise RuntimeError('unapproved transfer target')
        actual = available.get(str(path))
        if actual is None or any(actual.get(key) != row.get(key) for key in ('bytes','release','success_marker','verified_files','release_content_bytes')):
            raise RuntimeError('approved duplicate no longer matches verification')
        if path.is_symlink() or path.resolve() != path.absolute() or not path.is_file() or path.stat().st_size != row['bytes']:
            raise RuntimeError('unsafe or changed transfer file')
        targets.append((path, path.stat()))
    if sum(p.stat().st_size for p, _ in targets) != plan.get('bytes'):
        raise RuntimeError('approval total mismatch')
    return targets


def cleanup_main():
    raw = base64.b64decode(sys.argv[1], validate=True)
    if not re.fullmatch('[0-9a-f]{64}', sys.argv[2]) or hashlib.sha256(raw).hexdigest() != sys.argv[2]:
        raise RuntimeError('approval digest mismatch')
    plan = json.loads(raw)
    root = pathlib.Path('/opt/tp-community-actions')
    with (root/'deployment.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        with urllib.request.urlopen('http://127.0.0.1:9999/api/v1/sys_version', timeout=5) as response:
            version = json.load(response).get('data', {}).get('version', '')
        if not re.fullmatch('dev-[0-9a-f]{40}', version):
            raise RuntimeError('current backend identity unavailable')
        images = run(['docker','ps','--format','{{.Image}}'])
        active = set(re.findall(r'source-([0-9a-f]{40})', images)) | {version[4:]}
        if len(active) < 1 or 'tp-community-encore:source-' not in images:
            raise RuntimeError('current runtime identity unavailable')
        verified = historical_package_copies(root, active)
        targets = approved_targets(plan, verified, active)
        report = {'approval_sha256': sys.argv[2], 'active_commits': sorted(active),
                  'before_free_bytes': shutil.disk_usage(str(root)).free, 'files': []}
        # All targets passed complete release/hash/marker verification first.
        for path, before in targets:
            current = path.lstat()
            if path.is_symlink() or (current.st_ino,current.st_size,current.st_mtime_ns) != (before.st_ino,before.st_size,before.st_mtime_ns):
                raise RuntimeError('transfer changed after verification')
        try:
            for path, before in targets:
                path.unlink()
                report['files'].append({'package': str(path), 'bytes': before.st_size, 'verified_copy': True, 'deleted': True})
        finally:
            report['after_free_bytes'] = shutil.disk_usage(str(root)).free
            report['deleted_bytes'] = sum(r['bytes'] for r in report['files'])
            report['complete'] = len(report['files']) == len(targets)
            print(json.dumps(report), flush=True)


if __name__ == '__main__':
    cleanup_main()
