"""Verified Actions artifact transfer. Credentials and signed URLs never enter logs."""
import argparse
import hashlib
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile

NAME = re.compile(r"(?:encore|backend|broker)-[0-9a-f]{40}\.(?:tar\.gz|json)")
INCOMING = pathlib.Path('/opt/tp-community-actions/incoming')


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def validate_files(files):
    if not isinstance(files, dict) or not 1 <= len(files) <= 3:
        raise ValueError('invalid file manifest')
    for name, meta in files.items():
        if not NAME.fullmatch(name) or not isinstance(meta, dict):
            raise ValueError('invalid package name')
        if type(meta.get('bytes')) is not int or not 0 < meta['bytes'] <= 1024 ** 3:
            raise ValueError('invalid package size')
        if not re.fullmatch('[0-9a-f]{64}', meta.get('sha256', '')):
            raise ValueError('invalid package digest')


def valid_existing(root, name, meta):
    p = root / name
    return (not p.is_symlink() and p.is_file() and p.stat().st_size == meta['bytes']
            and digest(p) == meta['sha256'])


def extract_verified(archive, files, root):
    validate_files(files)
    with zipfile.ZipFile(archive) as z:
        infos = z.infolist()
        if len(infos) != len(files) or {x.filename for x in infos} != set(files):
            raise ValueError('unexpected artifact members')
        for info in infos:
            mode = (info.external_attr >> 16) & 0o170000
            if info.is_dir() or mode not in (0, 0o100000) or info.file_size != files[info.filename]['bytes']:
                raise ValueError('invalid artifact member')
        # Validate every member before publishing any package.
        with tempfile.TemporaryDirectory(prefix='.artifact-verify-', dir=root) as work:
            for info in infos:
                p = pathlib.Path(work) / info.filename
                with z.open(info) as src, p.open('wb') as dst:
                    count = 0
                    while block := src.read(1024 * 1024):
                        count += len(block)
                        if count > files[info.filename]['bytes']:
                            raise ValueError('oversized member')
                        dst.write(block)
                if not valid_existing(pathlib.Path(work), info.filename, files[info.filename]):
                    raise ValueError('package checksum mismatch')
            for name in files:
                if not valid_existing(root, name, files[name]):
                    os.replace(pathlib.Path(work) / name, root / name)
                print('Verified package: ' + name, flush=True)


def receive(request, root=INCOMING):
    files = request['files']
    validate_files(files)
    if root.is_symlink() or not root.is_dir():
        raise ValueError('invalid incoming directory')
    if all(valid_existing(root, name, meta) for name, meta in files.items()):
        print('Packages already verified; download skipped', flush=True)
        return
    url = request['url']
    parsed = urllib.parse.urlsplit(url)
    host = parsed.hostname or ''
    if (parsed.scheme != 'https' or parsed.username or parsed.password or parsed.port not in (None, 443)
            or not (host.endswith('.blob.core.windows.net') or host.endswith('.actions.githubusercontent.com'))):
        raise ValueError('invalid artifact download host')
    limit = sum(m['bytes'] for m in files.values()) + 8 * 1024 * 1024
    started = time.monotonic()
    # Do not follow redirects to arbitrary hosts or log exceptions containing signed URLs.
    opener = urllib.request.build_opener(NoRedirect())
    with tempfile.TemporaryFile(dir=root) as archive:
        with opener.open(url, timeout=60) as response:
            count, next_progress = 0, 5 * 1024 * 1024
            while block := response.read(1024 * 1024):
                count += len(block)
                if count > limit or time.monotonic() - started > 1800:
                    raise ValueError('artifact transfer limit exceeded')
                archive.write(block)
                if count >= next_progress:
                    print('Artifact received bytes: ' + str(count), flush=True)
                    next_progress += 5 * 1024 * 1024
        archive.seek(0)
        extract_verified(archive, files, root)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def send(args):
    files = {p.name: {'bytes': p.stat().st_size, 'sha256': digest(p)} for p in map(pathlib.Path, args.files)}
    validate_files(files)
    repo = os.environ['GITHUB_REPOSITORY']
    token = os.environ['GH_TOKEN']
    headers = {'Authorization': 'Bearer ' + token, 'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28'}
    api = 'https://api.github.com/repos/' + repo
    artifact_id = args.artifact_id
    if not str(artifact_id).isdigit():
        raise ValueError('invalid artifact ID')
    req = urllib.request.Request(api + '/actions/artifacts/' + str(artifact_id) + '/zip', headers=headers)
    try:
        urllib.request.build_opener(NoRedirect()).open(req, timeout=60)
        raise ValueError('artifact redirect missing')
    except urllib.error.HTTPError as err:
        if err.code != 302:
            raise ValueError('artifact URL request failed') from None
        url = err.headers['Location']
    # Only the short-lived download URL crosses SSH stdin, never GITHUB_TOKEN.
    payload = json.dumps({'url': url, 'files': files}).encode()
    command = ['ssh', '-i', args.key, '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
               '-o', 'UserKnownHostsFile=deploy/community-test/known_hosts', '-o', 'ServerAliveInterval=20',
               '-o', 'ServerAliveCountMax=9', 'root@47.92.253.145',
               'python3 /opt/tp-community-actions/incoming/artifact_transfer.py --receive']
    print('Transferring verified packages: ' + ', '.join(files), flush=True)
    subprocess.run(command, input=payload, check=True, timeout=1920)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--receive', action='store_true')
    parser.add_argument('--key')
    parser.add_argument('--artifact-id')
    parser.add_argument('files', nargs='*')
    args = parser.parse_args()
    try:
        if args.receive:
            receive(json.loads(sys.stdin.buffer.read(16384)))
        else:
            send(args)
    except Exception as err:
        # urllib exception messages can contain signed query strings. Print type only.
        print('Verified artifact transfer failed (' + type(err).__name__ + '); existing packages retained', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
