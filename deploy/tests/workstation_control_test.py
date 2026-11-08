import contextlib
import importlib.machinery
import importlib.util
import io
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = pathlib.Path(__file__).resolve().parents[2]
loader = importlib.machinery.SourceFileLoader('control', str(ROOT / 'deploy/scripts/gnotes-control'))
spec = importlib.util.spec_from_loader(loader.name, loader)
control = importlib.util.module_from_spec(spec)
loader.exec_module(control)


class WorkstationControlTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(dir='/tmp')
        self.addCleanup(temporary.cleanup)
        self.folder = pathlib.Path(temporary.name)
        self.key = self.folder / 'original.key'
        self.key.write_bytes(b'a' * 64 + b'\n')
        self.key.chmod(0o600)
        self.destination = self.folder / 'usb'
        self.destination.mkdir()

    def test_read_key_rejects_public_permissions_symlinks_and_invalid_data(self):
        self.key.chmod(0o644)
        with self.assertRaises(ValueError):
            control.read_key(self.key)
        self.key.chmod(0o600)
        link = self.folder / 'link'
        link.symlink_to(self.key)
        with self.assertRaises(OSError):
            control.read_key(link)
        for value in (b'', b'z' * 64, b'a' * 131):
            self.key.write_bytes(value)
            with self.assertRaises(ValueError):
                control.read_key(self.key)

    def test_backup_private_verified_and_never_overwrites_different_key(self):
        output = io.StringIO()
        # The managed test environment can expose a synthetic /tmp/.git marker.
        # Ignore only that ancestor marker; test real repository rejection below.
        exists = pathlib.Path.exists
        with patch.object(pathlib.Path, 'exists', lambda p: False if str(p) == '/tmp/.git' else exists(p)), \
                contextlib.redirect_stdout(output):
            self.assertEqual(control.backup_key(self.key, self.destination), 0)
            self.assertEqual(control.backup_key(self.key, self.destination), 0)
        target = self.destination / 'gnotes-recovery/database-v1.key'
        self.assertEqual(target.read_bytes(), self.key.read_bytes())
        self.assertEqual(target.stat().st_mode & 0o777, 0o600)
        self.assertEqual(target.parent.stat().st_mode & 0o777, 0o700)
        self.assertNotIn('a' * 64, output.getvalue())
        target.write_bytes(b'b' * 64)
        with patch.object(pathlib.Path, 'exists', lambda p: False if str(p) == '/tmp/.git' else exists(p)), self.assertRaises(ValueError):
            control.backup_key(self.key, self.destination)
        self.assertEqual(target.read_bytes(), b'b' * 64)

    def test_backup_rejects_git_repository_and_symlink_folder(self):
        (self.destination / '.git').mkdir()
        with self.assertRaises(ValueError):
            control.backup_key(self.key, self.destination)
        other = self.folder / 'other'
        other.mkdir(mode=0o700)
        (other / 'gnotes-recovery').symlink_to(self.destination)
        exists = pathlib.Path.exists
        with patch.object(pathlib.Path, 'exists', lambda p: False if str(p) == '/tmp/.git' else exists(p)), self.assertRaises(ValueError):
            control.backup_key(self.key, other)

    def test_healthy_recovery_never_reads_or_sends_key(self):
        with patch.object(control, 'public_health', return_value=(True, 'healthy')), \
                patch.object(control, 'read_key') as read, patch.object(control, 'ssh_run') as ssh, \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(control.recover(self.key), 0)
        read.assert_not_called()
        ssh.assert_not_called()

    def test_missing_key_recovery_uses_stdin_and_checks_public_health(self):
        remote = subprocess.CompletedProcess([], 0, b'SERVICE=inactive\nKEY=missing\nLOCAL=unavailable\n', b'')
        unlocked = subprocess.CompletedProcess([], 0, b'ok', b'')
        with patch.object(control, 'public_health', side_effect=[(False, 'HTTP 502'), (True, 'healthy')]), \
                patch.object(control, 'ssh_run', side_effect=[remote, unlocked]) as ssh, \
                contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(control.recover(self.key), 0)
        self.assertEqual(ssh.call_args.args, (control.UNLOCK,))
        self.assertEqual(ssh.call_args.kwargs['input'], self.key.read_bytes())
        self.assertNotIn('a' * 64, str(ssh.call_args.args))

    def test_recovery_does_not_send_key_on_ssh_failure_or_when_key_exists(self):
        for code, fields in [(255, b''), (0, b'KEY=present\nLOCAL=unavailable\n'),
                             (0, b'KEY=missing\nLOCAL=healthy\n')]:
            with self.subTest(fields=fields), \
                    patch.object(control, 'public_health', return_value=(False, 'HTTP 502')), \
                    patch.object(control, 'ssh_run', return_value=subprocess.CompletedProcess([], code, fields, b'')), \
                    patch.object(control, 'read_key') as read, contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaises(ValueError):
                    control.recover(self.key)
                read.assert_not_called()

    def test_failed_unlock_output_cannot_leak_key(self):
        responses = [subprocess.CompletedProcess([], 0, b'KEY=missing\nLOCAL=unavailable\n', b''),
                     subprocess.CompletedProcess([], 1, self.key.read_bytes(), self.key.read_bytes())]
        output = io.StringIO()
        with patch.object(control, 'public_health', return_value=(False, 'HTTP 502')), \
                patch.object(control, 'ssh_run', side_effect=responses), contextlib.redirect_stdout(output):
            with self.assertRaises(ValueError) as error:
                control.recover(self.key)
        self.assertNotIn('a' * 64, str(error.exception) + output.getvalue())


if __name__ == '__main__':
    unittest.main()
