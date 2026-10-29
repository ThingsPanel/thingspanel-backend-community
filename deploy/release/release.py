"""Tag selection and published-image receipts; never choose a moving branch."""
import argparse
import json
import os
import pathlib
import re
import subprocess


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def version(value):
    if not re.fullmatch(r'v\d+\.\d+\.\d+(?:\.\d+)?(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', value):
        raise ValueError('An explicit v-prefixed release tag is required; latest is forbidden')
    return value


def inspect_image(reference):
    result = json.loads(run('docker', 'buildx', 'imagetools', 'inspect', reference,
                            '--format', '{{json .Manifest}}'))
    platforms = {m.get('platform', {}).get('os', '') + '/' + m.get('platform', {}).get('architecture', '')
                 for m in result.get('manifests', [])}
    if not {'linux/amd64', 'linux/arm64'} <= platforms:
        raise ValueError('Published image lacks required architectures: ' + reference)
    if not re.fullmatch(r'sha256:[0-9a-f]{64}', result.get('digest', '')):
        raise ValueError('Published image digest missing: ' + reference)
    return {'reference': reference, 'digest': result['digest'], 'platforms': sorted(platforms)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=['select-tag', 'record-image'])
    parser.add_argument('--version', required=True)
    parser.add_argument('--component')
    parser.add_argument('--image')
    args = parser.parse_args()
    tag = version(args.version)
    if args.command == 'select-tag':
        subprocess.run(['git', 'fetch', 'origin', 'refs/tags/' + tag + ':refs/tags/' + tag], check=True)
        subprocess.run(['git', 'checkout', '--detach', 'refs/tags/' + tag], check=True)
        with open(os.environ['GITHUB_ENV'], 'a', encoding='utf-8') as env:
            env.write('VERSION=' + tag + '\nOWNER_LC=' + os.environ['GITHUB_REPOSITORY_OWNER'].lower() + '\n')
    else:
        if args.component not in {'frontend', 'backend', 'broker', 'encore'}:
            raise ValueError('Unknown release component')
        if not re.fullmatch(r'thingspanel-[a-z-]+', args.image or ''):
            raise ValueError('Invalid image repository')
        owner = os.environ['GITHUB_REPOSITORY_OWNER'].lower()
        names = ['docker.io/thingspanel/' + args.image, 'ghcr.io/' + owner + '/' + args.image,
                 'registry.cn-hangzhou.aliyuncs.com/thingspanel/' + args.image]
        receipt = {'component': args.component, 'version': tag, 'commit': run('git', 'rev-parse', 'HEAD'),
                   'images': [inspect_image(name + ':' + tag) for name in names]}
        target = pathlib.Path('release-records')
        target.mkdir(exist_ok=True)
        (target / (args.component + '-image.json')).write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
