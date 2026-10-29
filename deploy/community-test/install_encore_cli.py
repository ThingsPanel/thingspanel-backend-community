"""Install the checksum-pinned Encore CLI in the deployment tools directory."""
import hashlib
import os
import pathlib
import tarfile

ROOT = pathlib.Path('/opt/tp-community-actions')
SHA = '46add1a88eb02349e4ce51269fdade91c52e8fc044c972cc0ae40129a1eb91b2'
tools = ROOT / 'tools/encore-1.58.6'
marker = tools / '.archive-sha256'
if marker.is_file() and marker.read_text().strip() == SHA and (tools / 'bin/encore').is_file():
    raise SystemExit(0)
archive_path = ROOT / 'incoming/encore-1.58.6-linux_amd64.tar.gz'
if hashlib.sha256(archive_path.read_bytes()).hexdigest() != SHA:
    raise RuntimeError('Encore CLI archive checksum mismatch')
os.umask(0o077)
tools.mkdir(parents=True, exist_ok=True)
with tarfile.open(str(archive_path)) as archive:
    for member in archive.getmembers():
        dest = (tools / member.name).resolve()
        if (member.issym() or member.islnk() or not (member.isfile() or member.isdir())
                or not (dest == tools.resolve() or str(dest).startswith(str(tools.resolve()) + '/'))):
            raise RuntimeError('Unsafe CLI archive')
    archive.extractall(str(tools))
marker.write_text(SHA + '\n')
print('Encore CLI 1.58.6 installed and checksum verified')
