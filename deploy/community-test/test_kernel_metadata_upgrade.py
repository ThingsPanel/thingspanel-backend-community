"""Upgrade populated v16 fixtures, never an application database.

Set KERNEL_MIGRATION_FIXTURE_CONTAINER to a dedicated PostgreSQL test container.
Every fixture lives in a new schema inside a transaction that is rolled back.
"""
import os
import pathlib
import re
import subprocess
import unittest
import uuid

MIGRATIONS = pathlib.Path(__file__).resolve().parents[2] / 'encore-api/devicemetadata/migrations'
TENANT = '11111111-1111-4111-8111-111111111111'
MODEL = '22222222-2222-4222-8222-222222222222'
SNAPSHOT = '33333333-3333-4333-8333-333333333333'
TEMPLATE = '44444444-4444-4444-8444-444444444444'


class PopulatedMetadataUpgradeTest(unittest.TestCase):
    def setUp(self):
        self.container = os.environ.get('KERNEL_MIGRATION_FIXTURE_CONTAINER')
        if not self.container:
            self.skipTest('dedicated PostgreSQL fixture container required')
        if not re.fullmatch(r'[a-zA-Z0-9_.-]+', self.container):
            self.fail('invalid fixture container identifier')
        files = sorted(MIGRATIONS.glob('*.up.sql'))
        self.before = '\n'.join(p.read_text(encoding='utf-8') for p in files if int(p.name.split('_')[0]) <= 16)
        self.upgrade = '\n'.join(p.read_text(encoding='utf-8') for p in files if int(p.name.split('_')[0]) > 16)

    def execute(self, body):
        schema = 'kernel_upgrade_' + uuid.uuid4().hex
        script = 'BEGIN; CREATE SCHEMA "{0}"; SET LOCAL search_path TO "{0}",public;\n'.format(schema)
        script += self.before + '\n' + body + '\nROLLBACK;\n'
        result = subprocess.run(['docker', 'exec', '-i', self.container, 'psql', '-X', '-q',
                                 '-v', 'ON_ERROR_STOP=1', '-U', 'notification_test', '-d', 'notification_test'],
                                input=script, universal_newlines=True, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, timeout=90)
        self.assertEqual(result.returncode, 0, result.stderr[-3000:])

    def seed(self, template_tenant=TENANT):
        return """
INSERT INTO thing_models(id,tenant_id,name) VALUES ('{model}','{tenant}','retained model');
INSERT INTO thing_model_snapshots(id,thing_model_id,tenant_id,version,content,published_by)
VALUES ('{snapshot}','{model}','{tenant}',1,'{{"preserve":"original"}}','{tenant}');
INSERT INTO device_templates(id,tenant_id,name,thing_model_id,thing_model_snapshot_id)
VALUES ('{template}','{template_tenant}','existing template','{model}','{snapshot}');
""".format(model=MODEL, tenant=TENANT, snapshot=SNAPSHOT, template=TEMPLATE, template_tenant=template_tenant)

    def test_upgrade_preserves_existing_bindings_and_deleted_model_snapshots(self):
        assertions = """
DO $check$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM device_access_templates WHERE id='{template}' AND thing_model_id='{model}'
               AND thing_model_snapshot_id='{snapshot}') THEN RAISE EXCEPTION 'existing binding changed'; END IF;
 BEGIN
  INSERT INTO thing_models(id,tenant_id,name) VALUES (gen_random_uuid(),'{tenant}','retained model');
  RAISE EXCEPTION 'duplicate live name accepted';
 EXCEPTION WHEN unique_violation THEN NULL; END;
 UPDATE thing_models SET deleted_at=NOW() WHERE id='{model}';
 INSERT INTO thing_models(id,tenant_id,name) VALUES (gen_random_uuid(),'{tenant}','retained model');
 IF NOT EXISTS(SELECT 1 FROM thing_model_snapshots WHERE id='{snapshot}' AND content->>'preserve'='original')
 THEN RAISE EXCEPTION 'published snapshot changed'; END IF;
 IF NOT EXISTS(SELECT 1 FROM device_access_templates WHERE id='{template}' AND thing_model_snapshot_id='{snapshot}')
 THEN RAISE EXCEPTION 'deletion damaged existing binding'; END IF;
END;
$check$;
""".format(model=MODEL, tenant=TENANT, snapshot=SNAPSHOT, template=TEMPLATE)
        self.execute(self.seed() + self.upgrade + assertions)

    def test_inconsistent_old_tenant_reference_rejects_upgrade_without_rewriting_rows(self):
        foreign = '55555555-5555-4555-8555-555555555555'
        check = """
DO $check$
BEGIN
 BEGIN
  EXECUTE $upgrade${upgrade}$upgrade$;
  RAISE EXCEPTION 'inconsistent legacy reference silently accepted';
 EXCEPTION WHEN foreign_key_violation THEN NULL; END;
 IF to_regclass('device_templates') IS NULL OR to_regclass('device_access_templates') IS NOT NULL
 THEN RAISE EXCEPTION 'failed upgrade left partial schema'; END IF;
 IF NOT EXISTS(SELECT 1 FROM device_templates WHERE id='{template}' AND tenant_id='{foreign}')
 THEN RAISE EXCEPTION 'failed upgrade rewrote the legacy row'; END IF;
END;
$check$;
""".format(upgrade=self.upgrade, template=TEMPLATE, foreign=foreign)
        self.execute(self.seed(foreign) + check)


if __name__ == '__main__':
    unittest.main()
