"""Explicit fresh system-volume frontend stage with APFS CoW dependency clones."""
import ctypes
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import time


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify_dependency_versions(repo, seed, stage, workspace_dirs):
    """Verify workspace-local resolution; never accept a root fallback version."""
    versions = {}
    for relative in [Path('.'), *workspace_dirs]:
        declaration = json.loads((repo / relative / 'package.json').read_text())
        names = set(declaration.get('dependencies', {})) | set(declaration.get('devDependencies', {}))
        for name in sorted(names):
            old = seed.parent / relative / 'node_modules' / name / 'package.json'
            new = stage / relative / 'node_modules' / name / 'package.json'
            if not old.is_file() or not new.is_file():
                raise ValueError('Declared dependency is missing from copied graph: ' + str(relative) + ':' + name)
            old_version = json.loads(old.read_text())['version']
            if json.loads(new.read_text())['version'] != old_version:
                raise ValueError('Copied dependency version mismatch: ' + str(relative) + ':' + name)
            versions[str(relative) + ':' + name] = old_version
    return versions


def prepare(root, source, stage_value, seed_value, resume=False):
    stage = Path(stage_value).expanduser().resolve()
    allowed = Path.home() / '.codex/local-runtimes'
    if not str(stage).startswith(str(allowed) + '/') or '500G' in str(stage):
        raise ValueError('System frontend stage must be a new task directory under ~/.codex/local-runtimes')
    existing = stage.exists()
    if existing and not resume:
        raise ValueError('System frontend stage must be new unless explicit private-stage resume is requested')
    previous = {}
    if existing:
        proof = stage / 'LOCAL-STAGE-PROVENANCE.json'
        if not proof.is_file():
            raise ValueError('Cannot resume a stage without task provenance')
        previous = json.loads(proof.read_text())
        if previous.get('source_sha') != source['sha'] or previous.get('mode') != 'apfs-clonefile-no-copy-fallback':
            raise ValueError('Private stage provenance/source mismatch')
    seed = Path(seed_value).expanduser().resolve(strict=True)
    if '500G' in str(seed) or seed.name != 'node_modules':
        raise ValueError('Dependency seed must be existing node_modules outside damaged storage')
    repo = Path(source['path'])
    if sha(seed / '.pnpm/lock.yaml') != sha(repo / 'pnpm-lock.yaml'):
        raise ValueError('Dependency seed lockfile differs from pinned source')
    metadata = (seed / '.modules.yaml').read_text()
    if 'packageManager: pnpm@10.8.0' not in metadata or 'nodeLinker: isolated' not in metadata:
        raise ValueError('Only the verified matching pnpm 10.8 isolated layout is accepted')
    allowed.mkdir(mode=0o700, parents=True, exist_ok=True)
    if seed.stat().st_dev != allowed.stat().st_dev:
        raise ValueError('APFS clone requires same source/destination volume; no full-copy fallback')
    if shutil.disk_usage(allowed).free < 12 * 1024**3:
        raise ValueError('System frontend cloning requires at least 12 GiB free')
    if existing:
        if previous.get('lock_sha256') != sha(repo / 'pnpm-lock.yaml') or sha(stage / 'pnpm-lock.yaml') != sha(repo / 'pnpm-lock.yaml'):
            raise ValueError('Private stage lockfile changed')
        tracked = subprocess.check_output(['git', '-C', str(repo), 'ls-files', '-z']).decode().split('\0')
        for file in filter(None, tracked):
            if not (stage / file).is_file() or sha(stage / file) != sha(repo / file):
                raise ValueError('Private stage source differs: ' + file)
    else:
        stage.mkdir(mode=0o700)
    if not existing:
        archive = subprocess.Popen(['git', '-C', str(repo), 'archive', source['sha']], stdout=subprocess.PIPE)
        extract = subprocess.run(['tar', '-xf', '-', '-C', str(stage)], stdin=archive.stdout)
        archive.stdout.close()
        if extract.returncode or archive.wait():
            raise ValueError('Pinned frontend export failed')
    libc = ctypes.CDLL('/usr/lib/libSystem.B.dylib', use_errno=True)
    clonefile = libc.clonefile
    clonefile.argtypes = [ctypes.c_char_p, ctypes.c_char_p, ctypes.c_int]
    clonefile.restype = ctypes.c_int
    count = previous.get('files_cloned', 0)
    last = time.monotonic()
    def clone(src, dst):
        nonlocal count, last
        if count % 128 == 0 and shutil.disk_usage(stage).free < 4 * 1024**3:
            raise ValueError('Stopped dependency clone at 4 GiB system floor')
        if clonefile(os.fsencode(src), os.fsencode(dst), 0):
            raise OSError(ctypes.get_errno(), 'APFS clonefile failed; no copy fallback')
        count += 1
        if time.monotonic() - last > 20:
            print('Dependency clone: ' + str(count) + ' files (private CoW copies)', flush=True)
            last = time.monotonic()
        return dst
    dest = stage / 'node_modules'
    if not existing:
        shutil.copytree(seed, dest, symlinks=True, copy_function=clone)
    package_files = subprocess.check_output(['git', '-C', str(repo), 'ls-files', '-z', 'packages/*/package.json']).decode().split('\0')
    workspace_dirs = [Path(file).parent for file in package_files if file]
    module_roots = [dest]
    cloned_workspaces = []
    for relative in workspace_dirs:
        original_package = seed.parent / relative / 'package.json'
        if not original_package.is_file() or sha(original_package) != sha(repo / relative / 'package.json'):
            raise ValueError('Workspace manifest differs from the pinned source: ' + str(relative))
        old_modules = seed.parent / relative / 'node_modules'
        if old_modules.exists():
            if old_modules.is_symlink():
                raise ValueError('Workspace module root is unexpectedly a symlink')
            new_modules = stage / relative / 'node_modules'
            if new_modules.exists():
                if str(relative) not in previous.get('workspace_module_roots', []):
                    raise ValueError('Unrecorded preexisting workspace dependencies; refuse overwrite')
            else:
                shutil.copytree(old_modules, new_modules, symlinks=True, copy_function=clone)
            module_roots.append(new_modules)
            cloned_workspaces.append(str(relative))
        else:
            declaration = json.loads(original_package.read_text())
            if declaration.get('dependencies') or declaration.get('devDependencies'):
                raise ValueError('Workspace dependency seed missing: ' + str(relative))
    # Rebase root AND every workspace dependency graph. Omitting workspace links
    # silently falls back to incompatible root package versions.
    rewritten = 0
    for modules in module_roots:
        for directory, dirs, files in os.walk(modules, followlinks=False):
            for name in dirs + files:
                link = Path(directory) / name
                if not link.is_symlink():
                    continue
                raw = os.readlink(link)
                if os.path.isabs(raw):
                    target = Path(raw)
                    if target.is_relative_to(seed):
                        target = dest / target.relative_to(seed)
                    elif target.is_relative_to(seed.parent / 'packages'):
                        target = stage / 'packages' / target.relative_to(seed.parent / 'packages')
                    else:
                        raise ValueError('Absolute dependency symlink escapes verified seed/workspace')
                    link.unlink()
                    link.symlink_to(os.path.relpath(target, link.parent))
                    rewritten += 1
                resolved = link.resolve(strict=True)
                if not (resolved.is_relative_to(dest) or resolved.is_relative_to(stage / 'packages')):
                    raise ValueError('Dependency symlink escapes the fresh private stage')
        bindir = modules / '.bin'
        if bindir.is_dir():
            for path in bindir.iterdir():
                if path.is_file() and not path.is_symlink():
                    text = path.read_text()
                    changed = text.replace(str(seed.parent), str(stage))
                    if text != changed:
                        path.chmod(path.stat().st_mode | 0o200)
                        path.write_text(changed)
                        rewritten += 1
    versions = verify_dependency_versions(repo, seed, stage, workspace_dirs)
    # A later accidental install must not select or mutate the shared source store.
    meta = dest / '.modules.yaml'
    meta.chmod(meta.stat().st_mode | 0o200)
    meta.write_text(re.sub(r'^storeDir:.*$', 'storeDir: ' + str(root / 'cache/pnpm-store'), metadata, flags=re.MULTILINE))
    for executable in ('cross-env', 'vue-tsc', 'vite'):
        if not (dest / '.bin' / executable).is_file():
            raise ValueError('Dependency seed is incomplete: ' + executable)
    if sha(dest / '.pnpm/lock.yaml') != sha(stage / 'pnpm-lock.yaml'):
        raise ValueError('Cloned dependency lockfile changed')
    (stage / 'LOCAL-STAGE-PROVENANCE.json').write_text(json.dumps({'source_sha': source['sha'], 'lock_sha256': sha(stage / 'pnpm-lock.yaml'),
        'mode': 'apfs-clonefile-no-copy-fallback', 'files_cloned': count, 'paths_rebased': rewritten, 'workspace_module_roots': cloned_workspaces, 'verified_dependency_versions': versions}, indent=2))
    return stage
