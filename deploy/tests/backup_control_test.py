import gzip
import importlib.machinery
import importlib.util
import json
import os
import pathlib
import shutil
import sqlite3
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = pathlib.Path(__file__).resolve().parents[2]
loader = importlib.machinery.SourceFileLoader('backup_control', str(ROOT / 'deploy/scripts/gnotes-backup-control'))
spec = importlib.util.spec_from_loader(loader.name, loader)
control = importlib.util.module_from_spec(spec)
loader.exec_module(control)


class BackupControlTest(unittest.TestCase):
    def test_replication_preserves_only_storage_configuration(self):
        with patch.dict(os.environ, {
            'GNOTES_DATABASE_KEY_FILE': '/run/gnotes/database.key',
            'GNOTES_REQUIRE_ENCRYPTION': '1',
            'GNOTES_VOLATILE_KEY': '1',
            'GNOTES_STORAGE_TOOL': '/opt/gnotes/gnotes',
            'GNOTES_SETUP_TOKEN': 'private-setup',
            'RESTIC_PASSWORD': 'unrelated-password',
        }):
            env = control.environment({'RESTIC_PASSWORD': 'chosen-backup-password'})
        self.assertEqual(env['GNOTES_DATABASE_KEY_FILE'], '/run/gnotes/database.key')
        self.assertEqual(env['GNOTES_REQUIRE_ENCRYPTION'], '1')
        self.assertEqual(env['GNOTES_VOLATILE_KEY'], '1')
        self.assertEqual(env['GNOTES_STORAGE_TOOL'], '/opt/gnotes/gnotes')
        self.assertNotIn('GNOTES_SETUP_TOKEN', env)
        self.assertEqual(env['RESTIC_PASSWORD'], 'chosen-backup-password')

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        for name, value in {'APP': self.root / 'app', 'PRIVATE': self.root / 'private',
                            'RECOVERY': self.root / 'recovery/site.env'}.items():
            patcher = patch.object(control, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        control.JOB.update(busy=False, message='')
        self.data = dict(region='ap-northeast-2', bucket='test-backups', prefix='gnotes/new',
                         access_key='TESTACCESSKEY123456', secret_key='test-secret'.replace('-', '') * 4)
        control.atomic(control.RECOVERY, 'RESTIC_PASSWORD=fixture-site-key\n')

    def test_destination_restrictions(self):
        good = control.validate(self.data)
        self.assertEqual(good['RESTIC_REPOSITORY'], 's3:https://s3.ap-northeast-2.wasabisys.com/test-backups/gnotes/new')
        for field, value in [('region', 'localhost'), ('bucket', 'a/b'), ('bucket', '127.0.0.1'),
                             ('prefix', '../private'), ('prefix', 'gnotes/*'), ('prefix', 'x\nAWS_PROFILE=root'),
                             ('secret_key', 'x\nmalicious')]:
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                control.validate({**self.data, field: value})

    def test_probe_failure_preserves_existing_credentials(self):
        control.atomic(control.PRIVATE / 'wasabi.env', 'RESTIC_REPOSITORY=old\n')
        with patch.object(control, 'properties', return_value={'ActiveState': 'inactive'}), \
             patch.object(control, 'run', return_value=subprocess.CompletedProcess([], 12, b'', b'secret-error')) as run:
            with self.assertRaises(ValueError):
                control.configure(self.data)
            self.assertEqual(run.call_count, 1)
        self.assertEqual((control.PRIVATE / 'wasabi.env').read_text(), 'RESTIC_REPOSITORY=old\n')

    def test_publish_failure_rolls_back_files_and_schedule(self):
        old_values = 'RESTIC_REPOSITORY=old\nAWS_SECRET_ACCESS_KEY=old-private\n'
        control.atomic(control.PRIVATE / 'wasabi.env', old_values)
        control.atomic(control.PRIVATE / 'restic-password', 'old-password\n')
        control.atomic(control.PRIVATE / 'verified.json', '{"repository":"old"}')
        old_recovery = control.RECOVERY.read_text()
        original_atomic = control.atomic
        failed = False

        def fail_once(path, value):
            nonlocal failed
            if path.name == 'verified.json' and not failed:
                failed = True
                raise OSError('fixture write failed')
            original_atomic(path, value)

        with patch.object(control, 'properties', return_value={'ActiveState':'inactive', 'UnitFileState':'enabled'}), \
             patch.object(control, 'run', return_value=subprocess.CompletedProcess([], 0, b'', b'')), \
             patch.object(control, 'snapshots', return_value=[]), \
             patch.object(control, 'systemctl') as systemctl, patch.object(control, 'atomic', side_effect=fail_once):
            with self.assertRaises(OSError):
                control.configure(self.data)
            systemctl.assert_any_call('enable', '--now', 'gnotes-wasabi-backup.timer')
        self.assertEqual((control.PRIVATE / 'wasabi.env').read_text(), old_values)
        self.assertEqual(control.RECOVERY.read_text(), old_recovery)
        self.assertEqual((control.PRIVATE / 'restic-password').read_text(), 'old-password\n')

    def test_creation_requires_explicit_confirmation(self):
        with patch.object(control, 'properties', return_value={'ActiveState': 'inactive'}), \
             patch.object(control, 'run', return_value=subprocess.CompletedProcess([], 10, b'', b'')) as run:
            with self.assertRaises(ValueError):
                control.configure(self.data)
            self.assertEqual(run.call_count, 1)

    def test_enable_requires_verified_destination_and_saved_key(self):
        control.atomic(control.PRIVATE / 'wasabi.env', 'RESTIC_REPOSITORY=fixture\n')
        control.atomic(control.PRIVATE / 'verified.json', '{"repository":"other"}')
        with patch.object(control, 'systemctl') as systemctl:
            for saved in (False, True):
                with self.assertRaises(ValueError):
                    control.perform({'action': 'enable', 'recovery_saved': saved})
            systemctl.assert_not_called()

    def test_pause_and_stop_have_separate_meanings(self):
        with patch.object(control, 'systemctl') as systemctl:
            control.perform({'action': 'pause'})
            systemctl.assert_called_once_with('disable', '--now', 'gnotes-wasabi-backup.timer')
            systemctl.reset_mock()
            control.perform({'action': 'stop'})
            systemctl.assert_called_once_with('stop', 'gnotes-wasabi-backup.service')

    def test_secret_errors_are_not_published(self):
        with patch.object(control, 'perform', side_effect=RuntimeError('super-private-credential')):
            control.LOCK.acquire()
            control.worker({})
        self.assertNotIn('super-private', control.JOB['message'])
        self.assertFalse(control.JOB['busy'])
        self.assertEqual(control.JOB['outcome'], 'error')

    def test_local_recovery_evidence_and_failure_preserves_last_success(self):
        directory = control.APP / 'backups'
        directory.mkdir(parents=True)
        old = directory / 'gnotes-daily-20261018T080236Z.db.gz'
        latest = directory / 'gnotes-daily-20261019T080236Z.db.gz'
        old.write_bytes(b'old encrypted archive')
        latest.write_bytes(b'latest encrypted archive')
        with patch.dict(os.environ, {'GNOTES_DATABASE_KEY_FILE': '/private/key'}), \
             patch.object(control, 'run', return_value=subprocess.CompletedProcess([], 0, b'', b'')) as run, \
             patch.object(control, 'systemctl') as systemctl:
            control.perform({'action':'recovery-test'})
            command = run.call_args.args[0]
            self.assertEqual(command[1:4], ['db', 'recovery-test', '--source'])
            self.assertEqual(command[4], latest)
            systemctl.assert_not_called()
            evidence = (control.PRIVATE / 'recovery-checks.json').read_text()
            self.assertEqual(json.loads(evidence)['local']['archive'], latest.name)
            self.assertNotIn('/private/key', evidence)
            run.return_value = subprocess.CompletedProcess([], 1, b'', b'private error')
            with self.assertRaises(ValueError):
                control.perform({'action':'recovery-test'})
            self.assertEqual((control.PRIVATE / 'recovery-checks.json').read_text(), evidence)
        self.assertEqual(latest.read_bytes(), b'latest encrypted archive')

    def test_local_status_and_schedule_do_not_require_cloud_credentials(self):
        directory = control.APP / 'backups'
        directory.mkdir(parents=True)
        archive = directory / 'gnotes-daily-20261019T080236Z.db.gz'
        archive.write_bytes(b'encrypted archive')
        (directory / 'gnotes-daily-20990101T000000Z.db.gz').symlink_to(archive)
        with patch.object(control, 'properties', return_value={'ActiveState':'active','UnitFileState':'enabled','Result':'success'}):
            info = control.status()
        self.assertFalse(info['configured'])
        self.assertEqual(info['local']['count'], 1)
        self.assertEqual(info['local']['latest']['name'], archive.name)
        self.assertTrue(info['local']['schedule_enabled'])
        self.assertEqual(info['recovery_checks'], {})
        with patch.object(control, 'systemctl') as systemctl:
            control.perform({'action':'local-pause'})
            systemctl.assert_called_once_with('disable','--now','gnotes-podman-backup.timer')
        with patch.object(control, 'systemctl') as systemctl:
            control.perform({'action':'local-run'})
            systemctl.assert_any_call('start','--no-block','gnotes-podman-backup.service')
        self.assertEqual(archive.read_bytes(), b'encrypted archive')

    def test_offsite_recovery_downloads_then_tests_and_records_snapshot(self):
        control.atomic(control.PRIVATE / 'wasabi.env','RESTIC_REPOSITORY=fixture\n')
        def run(args, env=None, timeout=120):
            if args[1] == 'restore':
                staging = pathlib.Path(args[-1])
                (staging / 'gnotes-daily-20261019T080236Z.db.gz').write_bytes(b'encrypted downloaded archive')
            return subprocess.CompletedProcess(args,0,b'',b'')
        with patch.dict(os.environ, {'GNOTES_DATABASE_KEY_FILE':'/private/key'}), \
             patch.object(control,'snapshots',return_value=[{'id':'abcdef123456','time':'fixture'}]), \
             patch.object(control,'run',side_effect=run) as execute:
            control.perform({'action':'offsite-test'})
            self.assertEqual(execute.call_count,2)
        evidence = json.loads((control.PRIVATE / 'recovery-checks.json').read_text())['offsite']
        self.assertEqual(evidence['snapshot'],'abcdef123456')
        self.assertEqual(evidence['key_source'],'server')

    @unittest.skipUnless(shutil.which('restic'), 'Restic required')
    def test_real_repository_setup_and_restore(self):
        bin_dir = control.APP / 'bin'
        bin_dir.mkdir(parents=True)
        (control.APP / 'backups').mkdir()
        database = self.root / 'fixture.db'
        db = sqlite3.connect(database)
        for table, columns in {'users':'username TEXT, password_hash BLOB', 'notes':'user_id INTEGER,title TEXT,content TEXT',
                               'sessions':'token_hash TEXT', 'drafts':'user_id INTEGER', 'settings':'key TEXT',
                               'rate_limits':'scope TEXT', 'login_cooldowns':'username_hash TEXT', 'signup_events':'user_id INTEGER',
                               'invitations':'id INTEGER', 'security_daily':'day TEXT'}.items():
            db.execute(f'CREATE TABLE {table} ({columns})')
        db.execute("INSERT INTO notes VALUES(1,'restore me','private fixture')")
        db.commit()
        db.close()
        archive = control.APP / 'backups/gnotes-daily-20261010T030000Z.db.gz'
        with gzip.open(archive, 'wb') as stream:
            stream.write(database.read_bytes())
        for name in ('replicate-gnotes-wasabi', 'restore-gnotes-podman'):
            (bin_dir / name).symlink_to(ROOT / 'deploy/scripts' / name)
        backup = bin_dir / 'backup-gnotes-podman'
        backup.write_text('#!/bin/sh\nexit 0\n')
        backup.chmod(0o700)
        values = {'RESTIC_REPOSITORY': str(self.root / 'repository'), 'AWS_DEFAULT_REGION': 'ap-northeast-2',
                  'AWS_ACCESS_KEY_ID':'fixture', 'AWS_SECRET_ACCESS_KEY':'fixture-secret'}
        with patch.object(control, 'RESTIC', pathlib.Path(shutil.which('restic'))), \
             patch.object(control, 'validate', return_value=values), \
             patch.object(control, 'properties', return_value={'ActiveState':'inactive'}), \
             patch.object(control, 'systemctl'):
            # Ubuntu 24.04's Restic predates the distinct missing-repository exit code.
            # Initialize the test fixture explicitly; configure still performs the real
            # upload/check/download/restore round trip. Missing/create consent is tested above.
            env = control.environment({**values, 'RESTIC_PASSWORD': 'fixture-site-key'})
            self.assertEqual(control.run([control.RESTIC, 'init'], env).returncode, 0)
            control.configure({**self.data, 'allow_create': True})
            metadata = json.loads((control.PRIVATE / 'verified.json').read_text())
            self.assertEqual(len(metadata['snapshots']), 1)
            recovery = control.read_env(control.RECOVERY)
            self.assertEqual(recovery['RESTIC_PASSWORD'], 'fixture-site-key')
            self.assertNotIn('AWS_SECRET_ACCESS_KEY', recovery)
            self.assertEqual((control.PRIVATE / 'wasabi.env').stat().st_mode & 0o777, 0o600)
            control.perform({'action': 'enable', 'recovery_saved': True})
