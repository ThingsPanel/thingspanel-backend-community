import copy
import hashlib
import pathlib
import tempfile
import unittest
from encore_artifacts import NATIVE_IMAGE_SERVICES, validate_prebuilt_image, artifact_sha256, require_import_capacity


class NativeImageTests(unittest.TestCase):
    def setUp(self):
        self.commit='a'*40
        self.image='sha256:'+'b'*64
        self.metadata={'commit':self.commit,'image_id':self.image,'uncompressed_bytes':1024,'services':sorted(NATIVE_IMAGE_SERVICES)}
        self.manifest={'commit':self.commit,'sha256':{'image.json':'c'*64,'image.tar.gz':'d'*64}}

    def test_complete_candidate_and_loaded_image_match(self):
        self.assertEqual(validate_prebuilt_image(self.metadata,self.manifest,self.commit,self.image),self.image)

    def test_other_commit_or_retagged_image_is_rejected(self):
        with self.assertRaises(ValueError):
            validate_prebuilt_image(self.metadata,self.manifest,'e'*40)
        with self.assertRaises(ValueError):
            validate_prebuilt_image(self.metadata,self.manifest,self.commit,'sha256:'+'f'*64)

    def test_staged_image_cannot_masquerade_as_full_release(self):
        self.metadata['services']=['authn','thingmodel','automation','notification']
        with self.assertRaises(ValueError):
            validate_prebuilt_image(self.metadata,self.manifest,self.commit)

    def test_image_and_its_identity_must_both_be_checksummed(self):
        for path in ('image.json','image.tar.gz'):
            manifest=copy.deepcopy(self.manifest)
            del manifest['sha256'][path]
            with self.assertRaises(ValueError):
                validate_prebuilt_image(self.metadata,manifest,self.commit)

    def test_capacity_reserves_extraction_docker_staging_and_headroom(self):
        required=300+2*500+1024**3
        self.assertEqual(require_import_capacity(required,300,500),required)
        with self.assertRaises(RuntimeError):
            require_import_capacity(required-1,300,500)
        for invalid in (None,0,-1,'1000',True):
            self.metadata['uncompressed_bytes']=invalid
            with self.assertRaises(ValueError):
                validate_prebuilt_image(self.metadata,self.manifest,self.commit)

    def test_large_artifact_hash_uses_streaming_reads(self):
        data=b'a'*(3*1024*1024+7)
        with tempfile.TemporaryDirectory() as directory:
            path=pathlib.Path(directory)/'image.tar.gz'
            path.write_bytes(data)
            self.assertEqual(artifact_sha256(path),hashlib.sha256(data).hexdigest())


if __name__=='__main__':unittest.main()
