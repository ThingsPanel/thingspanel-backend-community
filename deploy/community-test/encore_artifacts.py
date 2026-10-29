"""Remove only completed CI inputs and unreferenced Encore source image tags."""
import json
import re

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
