import importlib.util,json,pathlib,tempfile,unittest
spec=importlib.util.spec_from_file_location('encore_artifacts',pathlib.Path(__file__).with_name('encore_artifacts.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class ArtifactBoundaryTest(unittest.TestCase):
    def test_keeps_running_images_and_uncompleted_or_unrelated_inputs(self):
        old,active,current,pending='a'*40,'b'*40,'c'*40,'d'*40
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory);(root/'incoming').mkdir();(root/'encore-releases'/old).mkdir(parents=True);(root/'encore-releases'/old/'deployment.json').write_text('{}')
            for name in ['encore-'+old+'.tar.gz','encore-'+pending+'.tar.gz','backend-'+old+'.tar.gz','config.private.json']:(root/'incoming'/name).write_text('fixture')
            removed_images=[]
            def run(args):
                if args==['docker','ps','-aq']:return 'container-a'
                if args==['docker','inspect','container-a']:return json.dumps([{'Image':'image-active'}])
                if args[:2]==['docker','images']:return '\n'.join(['tp-community-encore:source-'+old,'tp-community-encore:source-'+active,'tp-community-encore:source-'+current,'postgres:latest'])
                if args[:3]==['docker','image','inspect']:return json.dumps([{'Id':'image-active' if args[-1].endswith(active) else 'image-old'}])
                if args[:3]==['docker','image','rm']:removed_images.append(args[-1]);return ''
                raise AssertionError(args)
            module.prune_source_artifacts(root,current,run)
            self.assertEqual(removed_images,['tp-community-encore:source-'+old])
            self.assertFalse((root/'incoming'/('encore-'+old+'.tar.gz')).exists())
            for name in ['encore-'+pending+'.tar.gz','backend-'+old+'.tar.gz','config.private.json']:self.assertTrue((root/'incoming'/name).exists())

if __name__=='__main__':unittest.main()
