import contextlib
import hashlib
import io
import pathlib
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import artifact_transfer as transfer

NAME = 'encore-' + 'a' * 40 + '.tar.gz'
DATA = b'verified release fixture'
FILES = {NAME: {'bytes': len(DATA), 'sha256': hashlib.sha256(DATA).hexdigest()}}


def bundle(entries):
    out = io.BytesIO()
    with zipfile.ZipFile(out, 'w') as z:
        for name, value in entries:
            z.writestr(name, value)
    out.seek(0)
    return out


class ArtifactTransferTests(unittest.TestCase):
    def test_storage_artifact_uses_same_exact_member_and_digest_checks(self):
        name = 'storage-'+'b'*40+'.tar.gz'
        files = {name: FILES[NAME]}
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            transfer.extract_verified(bundle([(name, DATA)]), files, root)
            self.assertEqual((root/name).read_bytes(), DATA)
            with self.assertRaises(ValueError):
                transfer.extract_verified(bundle([(name,b'bad')]),files,root)
    def test_remote_helper_supports_python36_syntax(self):
        import ast
        source = pathlib.Path(transfer.__file__).read_text(encoding='utf-8-sig')
        ast.parse(source, feature_version=(3, 6))

    def test_validated_package_replaces_partial_atomically(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / NAME).write_bytes(b'partial previous upload')
            transfer.extract_verified(bundle([(NAME, DATA)]), FILES, root)
            self.assertEqual((root / NAME).read_bytes(), DATA)
            with patch('artifact_transfer.urllib.request.build_opener', side_effect=AssertionError('must not download')):
                transfer.receive({'files': FILES, 'url': 'unused'}, root)

    def test_bad_checksum_preserves_existing_package(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / NAME).write_bytes(b'previous')
            with self.assertRaises(ValueError):
                transfer.extract_verified(bundle([(NAME, b'x' * len(DATA))]), FILES, root)
            self.assertEqual((root / NAME).read_bytes(), b'previous')

    def test_archive_member_boundaries(self):
        cases = [[('../escape', DATA)], [(NAME, DATA), (NAME, DATA)], [(NAME, DATA + b'x')]]
        symlink = zipfile.ZipInfo(NAME)
        symlink.external_attr = 0o120777 << 16
        cases.append([(symlink, DATA)])
        for entries in cases:
            with self.subTest(entries=entries), tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(ValueError):
                    transfer.extract_verified(bundle(entries), FILES, pathlib.Path(directory))
                self.assertEqual(list(pathlib.Path(directory).iterdir()), [])

    def test_manifest_and_url_rejection_before_network(self):
        with tempfile.TemporaryDirectory() as directory, patch('artifact_transfer.urllib.request.build_opener') as opener:
            for url in ['http://x.blob.core.windows.net/a', 'https://evil.test/a',
                        'https://user:pass@x.blob.core.windows.net/a']:
                with self.assertRaises(ValueError):
                    transfer.receive({'files': FILES, 'url': url}, pathlib.Path(directory))
            opener.assert_not_called()
        for bad in [{'../' + NAME: FILES[NAME]}, {NAME: {'bytes': -1, 'sha256': 'a' * 64}}]:
            with self.assertRaises(ValueError):
                transfer.validate_files(bad)

    def test_runner_passes_signed_url_only_in_stdin(self):
        import argparse
        import urllib.error
        args = argparse.Namespace(files=[], artifact_id='12345', key='runner-private-key')
        redirect = urllib.error.HTTPError('https://api.github.com/test', 302, 'redirect',
                                         {'Location': 'https://x.blob.core.windows.net/a?signature=private'}, None)
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / NAME
            path.write_bytes(DATA)
            args.files = [str(path)]
            with patch.dict('os.environ', {'GH_TOKEN': 'runner-only-secret', 'GITHUB_REPOSITORY': 'owner/repo'}), \
                    patch('artifact_transfer.urllib.request.build_opener') as opener, \
                    patch('artifact_transfer.subprocess.run') as execute:
                opener.return_value.open.side_effect = redirect
                transfer.send(args)
                call = execute.call_args
                self.assertNotIn('signature=private', str(call.args[0]))
                self.assertNotIn(b'runner-only-secret', call.kwargs['input'])
                self.assertIn(b'signature=private', call.kwargs['input'])
                self.assertIn(b'"sha256"', call.kwargs['input'])

    def test_error_does_not_disclose_signed_url(self):
        output = io.StringIO()
        with patch('sys.argv', ['artifact_transfer.py', '--receive']), patch('sys.stdin') as stdin, \
                patch('artifact_transfer.receive', side_effect=ValueError('https://signed?secret=123')), \
                contextlib.redirect_stderr(output):
            stdin.buffer.read.return_value = b'{}'
            self.assertEqual(transfer.main(), 1)
        self.assertNotIn('secret', output.getvalue())


if __name__ == '__main__':
    unittest.main()

