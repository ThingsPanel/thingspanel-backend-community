"""Verify actual local Docker backing storage; never move or prune data."""
import json
import os
from pathlib import Path
import shutil

GIB = 1024**3
MINIMUM = {'runtime': 6, 'backend': 6, 'frontend-install': 6, 'frontend': 6, 'encore': 12, 'broker': 16}


def orbstack_internal(runtime):
    context = runtime.run(['docker', 'context', 'show'])
    if context != 'orbstack':
        raise ValueError('Internal bounded mode only supports the verified local OrbStack context')
    info = json.loads(runtime.run(['docker', 'context', 'inspect', context]))[0]
    endpoint = info['Endpoints']['docker']['Host']
    expected = Path.home() / '.orbstack/run/docker.sock'
    if endpoint != 'unix://' + str(expected):
        raise ValueError('OrbStack endpoint is not the expected local Unix socket')
    image = Path.home() / 'Library/Group Containers/HUAQ24HBR6.dev.orbstack/data/data.img.raw'
    actual = image.resolve(strict=True)
    if actual != image or not actual.is_file():
        raise ValueError('OrbStack backing file changed location; inspect actual storage before continuing')
    volume = Path('/System/Volumes/Data')
    if actual.stat().st_dev != volume.stat().st_dev:
        raise ValueError('OrbStack image is not on the expected internal APFS Data volume')
    return actual


def check(runtime, phase='runtime'):
    mode = os.environ.get('TP_DOCKER_STORAGE_MODE', 'external')
    if phase not in MINIMUM:
        raise ValueError('Unknown disk-budget phase')
    if mode == 'internal-bounded':
        if os.environ.get('TP_ALLOW_INTERNAL_BOUNDED_BUILD') != 'yes':
            raise ValueError('Explicit TP_ALLOW_INTERNAL_BOUNDED_BUILD=yes is required; no disk data will be moved')
        backing = orbstack_internal(runtime)
        required = MINIMUM[phase] * GIB
        if shutil.disk_usage(backing).free < required:
            raise ValueError(f'Internal OrbStack {phase} requires {MINIMUM[phase]} GiB free on APFS Data; no build/start allowed')
        return {'mode': mode, 'backing_file': str(backing), 'minimum_gib': MINIMUM[phase], 'floor_gib': 4}
    if mode != 'external':
        raise ValueError('Unknown Docker storage mode')
    verified = os.environ.get('TP_DOCKER_STORAGE_VERIFIED', '')
    if not verified or Path(verified).expanduser().resolve() != runtime.workspace_root():
        raise ValueError('Verify external Docker storage or explicitly select verified internal-bounded mode; no relocation is performed')
    return {'mode': mode, 'backing_file': None, 'minimum_gib': 6, 'floor_gib': 4}
