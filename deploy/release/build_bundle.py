"""Package only verified, matching release components; never publish partial bundles."""
import argparse
import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import tarfile
import tempfile

from release import inspect_image, run, version


def asset(repo, tag, name, target):
    subprocess.run(['gh', 'release', 'download', tag, '--repo', repo, '--pattern', name,
                    '--dir', str(target), '--clobber'], check=True)
    return target / name


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--version', required=True)
    args = parser.parse_args()
    tag = version(args.version)
    commit = run('git', 'rev-parse', 'HEAD')
    repository = os.environ['GITHUB_REPOSITORY']
    frontend = 'ThingsPanel/thingspanel-frontend-community'
    output = pathlib.Path('release-output')
    output.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory() as temp:
        root = pathlib.Path(temp)
        bundle = root / ('thingspanel-linux-' + tag)
        bundle.mkdir()
        images = {}
        receipts = {}
        expected = {'backend': 'thingspanel-go', 'broker': 'thingspanel-mosquitto',
                    'encore': 'thingspanel-encore', 'frontend': 'thingspanel-vue'}
        for component, image in expected.items():
            repo = frontend if component == 'frontend' else repository
            receipt = json.loads(asset(repo, tag, component + '-image.json', root).read_text())
            if receipt['version'] != tag or receipt['component'] != component:
                raise ValueError('Mismatched release receipt: ' + component)
            if component != 'frontend' and receipt['commit'] != commit:
                raise ValueError('Image does not match backend tag source: ' + component)
            reference = 'registry.cn-hangzhou.aliyuncs.com/thingspanel/' + image + ':' + tag
            verified = inspect_image(reference)
            recorded = next(item for item in receipt['images'] if item['reference'] == reference)
            if verified['digest'] != recorded['digest']:
                raise ValueError('Registry tag changed after build: ' + component)
            images[component] = reference.split(':', 1)[0] + '@' + verified['digest']
            receipts[component] = receipt
        # Require frontend assets from the same source as its published image.
        archive = asset(frontend, tag, 'thingspanel-frontend-' + tag + '.tar.gz', root)
        with tarfile.open(archive) as source:
            revision = source.extractfile('./SOURCE_COMMIT').read().decode().strip()
        if revision != receipts['frontend']['commit']:
            raise ValueError('Frontend assets and image have different source commits')
        for arch in ['amd64', 'arm64']:
            asset(repository, tag, 'encore-tools-linux-' + arch + '.tar.gz', bundle)
        templates = pathlib.Path('deploy/release/templates')
        shutil.copytree(templates, bundle, dirs_exist_ok=True)
        # Migrations are delivered as reviewed source, never auto-applied by up.
        for name, source in [('community', 'sql'), ('automation', 'encore-api/automation/migrations'),
                             ('device_metadata', 'encore-api/devicemetadata/migrations'),
                             ('notification', 'encore-api/l3-core/notification/migrations')]:
            shutil.copytree(source, bundle / 'migrations' / name)
        shutil.copy('deploy/community-test/automation_tick.py', bundle / 'automation_tick.test-host-reference.py')
        shutil.copy('deploy/community-test/notification_renew.py', bundle / 'notification_renew.test-host-reference.py')
        (bundle / 'images.env').write_text(''.join('TP_' + key.upper() + '_IMAGE=' + value + '\n'
                                                    for key, value in images.items()))
        (bundle / 'release-manifest.json').write_text(json.dumps({'version': tag, 'components': receipts}, indent=2))
        path = output / ('thingspanel-linux-' + tag + '.tar.gz')
        with tarfile.open(path, 'w:gz') as archive:
            archive.add(bundle, arcname=bundle.name)
        (output / 'SHA256SUMS').write_text(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + path.name + '\n')


if __name__ == '__main__':
    main()
