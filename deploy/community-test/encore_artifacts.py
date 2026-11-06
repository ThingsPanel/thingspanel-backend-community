"""Remove only completed CI inputs and unreferenced Encore source image tags."""
import json
import re
import shutil

def reclaim_build_cache(root,run):
    """Reclaim reproducible compiler output only when the build cannot start."""
    if shutil.disk_usage(str(root)).free >= 1024**3:
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
