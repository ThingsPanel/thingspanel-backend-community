import copy
import json
import pathlib
import tempfile
import unittest

from upgrade_quarantine import build_archive, write_private_archive


class ApprovedQuarantineTest(unittest.TestCase):
    def setUp(self):
        self.receipt = {'status':'restored-and-verified','archive_sha256':'source-sha',
                        'rehearsal_database':'tp_upgrade_rehearsal_1'}
        self.source = {'devices':[
            {'id':'unowned','tenant_id':None,'voucher':'private-device-credential'},
            {'id':'another-unowned','tenant_id':None},
            {'id':'device','tenant_id':'a','device_config_id':'template'}],
            'templates':[{'id':'template','tenant_id':'b','template_secret':'private-template-credential'}],
            'groups':[{'id':'group','tenant_id':'b'}],
            'members':[{'device_id':'device','group_id':'group','tenant_id':'a'}]}
        self.approval = {'source_archive_sha256':'source-sha','disposition':'preserve-for-later-claim',
            'entries':[
                {'reason':'missing-device-tenant','device_id':'unowned'},
                {'reason':'cross-tenant-template','device_id':'device','template_id':'template',
                 'device_tenant':'a','template_tenant':'b'},
                {'reason':'cross-tenant-group-member','device_id':'device','group_id':'group',
                 'device_tenant':'a','group_tenant':'b','member_tenant':'a'}]}

    def test_preserves_full_rows_without_mutation_or_expanding_scope(self):
        original = copy.deepcopy(self.source)
        archive = build_archive(self.source,self.approval,self.receipt)
        self.assertEqual(original,self.source)
        self.assertEqual(3,len(archive['entries']))
        self.assertEqual('private-device-credential',archive['entries'][0]['original_rows']['device']['voucher'])
        self.assertNotIn('another-unowned',json.dumps(archive))
        self.assertEqual('device-template-relationship',archive['entries'][1]['excluded_from_conversion']['entity'])
        self.assertEqual('group-membership',archive['entries'][2]['excluded_from_conversion']['entity'])

    def test_changed_ownership_or_relationship_and_wrong_snapshot_are_rejected(self):
        for change in ('owner','relationship','snapshot'):
            with self.subTest(change=change):
                source, approval = copy.deepcopy(self.source), copy.deepcopy(self.approval)
                if change == 'owner': source['devices'][0]['tenant_id']='a'
                elif change == 'relationship': source['devices'][2]['device_config_id']=None
                else: approval['source_archive_sha256']='other-source'
                with self.assertRaises(ValueError):build_archive(source,approval,self.receipt)

    def test_duplicate_decisions_rejected(self):
        self.approval['entries'].append(self.approval['entries'][0])
        with self.assertRaises(ValueError):build_archive(self.source,self.approval,self.receipt)

    def test_archive_is_idempotent_receipt_has_no_credentials_and_cannot_be_overwritten(self):
        archive = build_archive(self.source,self.approval,self.receipt)
        with tempfile.TemporaryDirectory() as directory:
            receipt = write_private_archive(directory,archive)
            self.assertEqual(receipt,write_private_archive(directory,archive))
            self.assertEqual(1,receipt['excluded_devices'])
            self.assertEqual(2,receipt['excluded_relationships'])
            self.assertNotIn('credential',json.dumps(receipt))
            self.assertFalse(receipt['migration_complete'])
            path = next(pathlib.Path(directory).glob('*.private.json'))
            self.assertEqual(archive,json.loads(path.read_text(encoding='utf-8')))
            archive['entries'][0]['original_rows']['device']['voucher']='changed'
            with self.assertRaises(RuntimeError):write_private_archive(directory,archive)


if __name__ == '__main__':unittest.main()
