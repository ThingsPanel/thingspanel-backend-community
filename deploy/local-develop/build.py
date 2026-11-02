"""Explicit phase builds. Output and caches stay on 80G; never auto-start services."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import time


def digest(path):
    hasher = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            hasher.update(block)
    return hasher.hexdigest()


def execute(command, cwd, env, root, runtime, phase):
    runtime.preflight(root, phase)
    log = root / 'cache' / (phase + '.build.log')
    if log.exists():
        previous = log.with_name(phase + '.build.previous-' + digest(log)[:12] + '.log')
        if not previous.exists():
            shutil.copyfile(log, previous)
    with log.open('wb') as output:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            while process.poll() is None:
                if shutil.disk_usage('/').free < 4 * 1024**3 or shutil.disk_usage('/Volumes/80G').free < 4 * 1024**3:
                    raise ValueError('Build stopped at disk safety floor; review private build log before retrying')
                time.sleep(2)
            if process.returncode:
                raise ValueError('Build failed; no runtime changes applied. See fixture cache/' + log.name)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()
    runtime.preflight(root, phase)


def encore_command(tag):
    # Official CLI defaults to amd64; never rely on host architecture inference.
    return ['encore', 'build', 'docker', tag, '--arch', 'arm64', '--os', 'linux', '--base', 'scratch',
        '--services=authn,thingmodel,devicemetadata,fileassets,connectorcatalog,automation,notification', '--skip-config']


def build(root, runtime, phase, base_image=None, system_stage=None, dependency_seed=None, resume_stage=False):
    runtime.preflight(root, phase)
    manifest = runtime.load_fixture(root)
    config = json.loads((root / 'compose.json').read_text())
    out = root / 'artifacts'
    out.mkdir(mode=0o700, exist_ok=True)
    cache = root / 'cache'
    env = os.environ.copy()
    for key, directory in {'GOCACHE': 'go-build', 'GOMODCACHE': 'go-mod', 'TMPDIR': 'tmp',
                           'XDG_CACHE_HOME': 'xdg', 'ENCORE_CACHE_DIR': 'encore', 'ENCORE_CONFIG_DIR': 'encore-config', 'PNPM_HOME': 'pnpm',
                           'GOPATH': 'gopath', 'npm_config_cache': 'npm-cache', 'COREPACK_HOME': 'corepack'}.items():
        path = cache / directory
        path.mkdir(parents=True, exist_ok=True)
        env[key] = str(path)
    env['ENCORE_DAEMON_LOG_PATH'] = str(cache / 'encore-daemon.log')
    env.update(GOMAXPROCS='1', GOFLAGS='-p=1', GOOS='linux', GOARCH='arm64', CGO_ENABLED='0')
    provenance_path = root / 'build-provenance.json'
    provenance = json.loads(provenance_path.read_text()) if provenance_path.exists() else {}
    source = manifest['frontend' if phase == 'frontend' else 'backend']
    sha, repo = source['sha'], Path(source['path'])
    if phase in ('backend', 'frontend'):
        if not base_image:
            raise ValueError('Provide --runtime-image already-local runtime image; no implicit pulls')
        image = json.loads(runtime.run(['docker', 'image', 'inspect', base_image]))[0]
        if image.get('Os') != 'linux' or image.get('Architecture') != 'arm64':
            raise ValueError('Runtime image must be linux/arm64')
        image_id = image['Id']
    if phase == 'backend':
        binary = out / 'ThingsPanel-Go'
        pending = out / 'ThingsPanel-Go.pending'
        execute(['go', 'build', '-trimpath', '-ldflags', '-X project/pkg/global.SYSTEM_VERSION=develop-' + sha,
                 '-o', str(pending), '.'], repo, env, root, runtime, phase)
        pending.chmod(0o500)
        os.replace(pending, binary)
        mounts = [str(binary) + ':/go/src/app/ThingsPanel-Go:ro']
        resources = {}
        for directory in ('configs', 'sql', 'static'):
            target = out / ('backend-' + directory)
            if target.exists():
                raise ValueError('Resource snapshot already exists; use a new fixture rather than overwrite resources')
            shutil.copytree(repo / directory, target)
            mounts.append(str(target) + ':/go/src/app/' + directory + ':ro')
            for path in target.rglob('*'):
                if path.is_file():
                    resources[str(path.relative_to(out))] = digest(path)
        config['services']['backend']['image'] = image_id
        config['services']['backend']['entrypoint'] = ['/go/src/app/ThingsPanel-Go']
        config['services']['backend']['volumes'] += mounts
        config['services']['automation-scheduler']['image'] = image_id
        provenance['backend'] = {'sha': sha, 'mode': 'readonly-binary-and-resource-overlay', 'base_image': image_id,
                                 'binary_sha256': digest(binary), 'resources': resources}
        provenance['automation-scheduler'] = {'sha': sha, 'mode': 'shell-only-runtime', 'base_image': image_id}
    elif phase == 'frontend':
        # Work in a tracked-source export: pnpm/node_modules never pollutes pinned checkout.
        if system_stage or dependency_seed:
            if not system_stage or not dependency_seed:
                raise ValueError('Both explicit system stage and matching dependency seed are required')
            from frontend_seed import prepare
            stage = prepare(root, source, system_stage, dependency_seed, resume_stage)
        else:
            stage = root / 'frontend-build'
            if not stage.exists():
                stage.mkdir(mode=0o700)
                archive = subprocess.Popen(['git', '-C', str(repo), 'archive', sha], stdout=subprocess.PIPE)
                result = subprocess.run(['tar', '-xf', '-', '-C', str(stage)], stdin=archive.stdout)
                archive.stdout.close()
                if result.returncode or archive.wait():
                    raise ValueError('Frontend tracked-source export failed')
        tracked = runtime.run(['git', '-C', str(repo), 'ls-files', '-z']).split('\0')
        for file in filter(None, tracked):
            if (repo / file).is_file() and (not (stage / file).is_file() or digest(repo / file) != digest(stage / file)):
                raise ValueError('Frontend source export differs from pinned checkout: ' + file)
        env.pop('GOOS', None)
        env.pop('GOARCH', None)
        if not dependency_seed:
            execute(['pnpm', 'install', '--frozen-lockfile', '--store-dir', str(cache / 'pnpm-store')], stage, env, root, runtime, 'frontend-install')
        else:
            for variable, name in [('TMPDIR', 'tmp'), ('XDG_CACHE_HOME', 'xdg')]:
                local_cache = stage / '.local-cache' / name
                local_cache.mkdir(mode=0o700, parents=True, exist_ok=True)
                env[variable] = str(local_cache)
        execute(['pnpm', 'build'], stage, env, root, runtime, 'frontend')
        dist = stage / 'dist'
        if not (dist / 'index.html').is_file():
            raise ValueError('Frontend build did not create index.html')
        if dependency_seed:
            exported = out / 'frontend-dist'
            if exported.exists():
                raise ValueError('Frontend output directory already exists; refuse overwrite')
            shutil.copytree(dist, exported)
            dist = exported
        config['services']['frontend']['image'] = image_id
        config['services']['frontend']['volumes'].append(str(dist) + ':/usr/share/nginx/html:ro')
        provenance['frontend'] = {'sha': sha, 'mode': 'readonly-dist-overlay', 'base_image': image_id,
            'dist_path': str(dist), 'files': {str(path.relative_to(dist)): digest(path) for path in dist.rglob('*') if path.is_file()}}
    elif phase == 'encore':
        version = runtime.run(['encore', 'version'])
        if not version.startswith('encore version v1.58.6'):
            raise ValueError('Encore CLI 1.58.6 is required')
        tag = 'tp-local-encore:' + sha
        execute(encore_command(tag),
            repo / 'encore-api', env, root, runtime, phase)
        image = json.loads(runtime.run(['docker', 'image', 'inspect', tag]))[0]
        if image.get('Os') != 'linux' or image.get('Architecture') != 'arm64':
            raise ValueError('Encore output must be linux/arm64; image not activated')
        for name in ('encore-api', 'notification-core'):
            config['services'][name]['image'] = image['Id']
            provenance[name] = {'sha': sha, 'mode': 'official-encore-build', 'base_image': image['Id'], 'cli': version}
    else:
        raise ValueError('Unknown build phase')
    # Recheck checkout identity after build; failed phases never activate configuration.
    runtime.load_fixture(root)
    from identity import atomic
    atomic(provenance_path, provenance)
    atomic(root / 'compose.json', config)
    (root / 'images.lock.json').unlink(missing_ok=True)
    print('Built ' + phase + ' from ' + sha + '; no containers started. Relock images before startup.')


def verify(root, manifest, config):
    path = root / 'build-provenance.json'
    if not path.exists():
        return {}
    provenance = json.loads(path.read_text())
    out = root / 'artifacts'
    for name, record in provenance.items():
        expected = manifest['frontend' if name == 'frontend' else 'backend']['sha']
        if record['sha'] != expected or config['services'][name]['image'] != record['base_image']:
            raise ValueError('Build provenance SHA/image mismatch: ' + name)
        mounts = config['services'][name].get('volumes', [])
        if name == 'backend':
            binary = out / 'ThingsPanel-Go'
            if digest(binary) != record['binary_sha256'] or str(binary) + ':/go/src/app/ThingsPanel-Go:ro' not in mounts:
                raise ValueError('Backend develop binary changed or readonly overlay missing')
            for file, expected_hash in record['resources'].items():
                if digest(out / file) != expected_hash:
                    raise ValueError('Backend source resource changed')
            for directory in ('configs', 'sql', 'static'):
                if str(out / ('backend-' + directory)) + ':/go/src/app/' + directory + ':ro' not in mounts:
                    raise ValueError('Backend source resource overlay missing')
        if name == 'frontend':
            dist = Path(record.get('dist_path', str(root / 'frontend-build/dist')))
            if not dist.is_relative_to(root):
                raise ValueError('Frontend runtime artifacts must remain within fixture on 80G')
            if str(dist) + ':/usr/share/nginx/html:ro' not in mounts:
                raise ValueError('Frontend develop asset overlay missing')
            actual = {str(p.relative_to(dist)): digest(p) for p in dist.rglob('*') if p.is_file()}
            if actual != record['files']:
                raise ValueError('Frontend built assets changed')
    return provenance
