"""Validate native release artifacts and retain legacy artifact helpers."""
import hashlib
import json
import re
import shutil
import pathlib

APPROVED_EXTRA_BUILD_CACHE = pathlib.Path('/root/.cache/encore-build')

NATIVE_IMAGE_SERVICES = {'authn','thingmodel','devicemetadata','fileassets','connectorcatalog',
                         'automation','notification','marketinstall','platformconfig'}


def validate_prebuilt_image(metadata, source_manifest, commit, loaded_image_id=None):
    if (not isinstance(metadata,dict) or not isinstance(source_manifest,dict)
            or metadata.get('commit') != commit or source_manifest.get('commit') != commit
            or not isinstance(metadata.get('services'),list)
            or not all(isinstance(service,str) for service in metadata['services'])
            or set(metadata['services']) != NATIVE_IMAGE_SERVICES
            or type(metadata.get('uncompressed_bytes')) is not int
            or metadata['uncompressed_bytes'] <= 0
            or not isinstance(metadata.get('image_id'),str)
            or not re.fullmatch('sha256:[0-9a-f]{64}',metadata['image_id'])
            or not {'image.json','image.tar.gz'} <= set(source_manifest.get('sha256',{}))):
        raise ValueError('Complete immutable native image is missing or belongs to a different source')
    if loaded_image_id is not None and loaded_image_id != metadata['image_id']:
        raise ValueError('Loaded image identity differs from verified CI image')
    return metadata['image_id']


def artifact_sha256(path):
    # Image bundles can exceed the entire memory available on the live host.
    digest=hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024*1024),b''):
            digest.update(block)
    return digest.hexdigest()


def import_capacity_required(extracted_bytes, image_bytes, headroom_bytes=1024**3):
    if type(headroom_bytes) is not int or headroom_bytes < 384 * 1024**2:
        raise ValueError('Native image import must retain at least 384 MiB headroom')
    return extracted_bytes + 2*image_bytes + headroom_bytes


def require_import_capacity(free_bytes, extracted_bytes, image_bytes, headroom_bytes=1024**3):
    # The incoming compressed package already exists. Reserve its extracted
    # contents, two image copies for Docker staging/layers, and configured headroom.
    required=import_capacity_required(extracted_bytes, image_bytes, headroom_bytes)
    if free_bytes < required:
        raise RuntimeError('Image import requires {} free bytes; {} available'.format(required,free_bytes))
    return required

def prepare_import_capacity(root, run, extracted_bytes, image_bytes, approved_extra_cache=None, headroom_bytes=1024**3):
    required = import_capacity_required(extracted_bytes, image_bytes, headroom_bytes)
    reclaim_build_cache(root, run, minimum_free_bytes=required)
    if approved_extra_cache is not None and shutil.disk_usage(str(root)).free < required:
        cache = pathlib.Path(approved_extra_cache)
        if cache != APPROVED_EXTRA_BUILD_CACHE or cache.is_symlink() or cache.resolve() != cache.absolute():
            raise RuntimeError('Unsafe additional build cache path')
        if cache.is_dir():
            # Exact explicitly approved reproducible Encore cache only. Never follow
            # symlink entries; rmtree unlinks these rather than traversing targets.
            shutil.rmtree(str(cache))
            cache.mkdir(mode=0o700)
    return require_import_capacity(shutil.disk_usage(str(root)).free, extracted_bytes, image_bytes, headroom_bytes)


def reclaim_build_cache(root,run,minimum_free_bytes=1024**3):
    """Reclaim reproducible compiler output only when the build cannot start."""
    if shutil.disk_usage(str(root)).free >= minimum_free_bytes:
        return False
    cache=root/'encore-build-cache'
    if cache.is_symlink() or cache.resolve().parent!=root.resolve():
        raise RuntimeError('Unsafe build cache path')
    if cache.is_dir():
        # go clean preserves cache bookkeeping and never touches module sources,
        # runtime configuration, releases, database volumes or running images.
        run(['env','GOCACHE='+str(cache),'/usr/local/go/bin/go','clean','-cache'])
        return True
    return False

def prune_source_artifacts(root,current,run):
    containers=run(['docker','ps','-aq']).split()
    used={item['Image'] for item in json.loads(run(['docker','inspect']+containers))} if containers else set()
    removed=[]
    for tag in run(['docker','images','--format','{{.Repository}}:{{.Tag}}']).splitlines():
        if not re.fullmatch('tp-community-encore:source-[0-9a-f]{40}',tag) or tag.endswith(current):continue
        image=json.loads(run(['docker','image','inspect',tag]))[0]['Id']
        if image in used:continue
        # No force: Docker also refuses removal if a container acquired it meanwhile.
        run(['docker','image','rm',tag]);removed.append(tag)
    incoming=(root/'incoming').resolve()
    for path in incoming.iterdir():
        match=re.fullmatch('encore-([0-9a-f]{40})\\.tar\\.gz',path.name)
        if not match or match[1]==current:continue
        if not (root/'encore-releases'/match[1]/'deployment.json').is_file():continue
        if path.is_symlink() or not path.is_file() or path.resolve().parent!=incoming:continue
        path.unlink();removed.append('incoming/'+path.name)
    return removed
