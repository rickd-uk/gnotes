import importlib.machinery
import importlib.util
import pathlib
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = pathlib.Path(__file__).resolve().parents[2]


def load(name):
    loader = importlib.machinery.SourceFileLoader(name, str(ROOT / 'deploy/scripts' / name))
    spec = importlib.util.spec_from_loader(loader.name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


prepare = load('prepare-gnotes-volatile-host')
unlock = load('unlock-gnotes-podman')


class VolatileKeyTest(unittest.TestCase):
    def test_swap_and_memory_headroom(self):
        empty = 'Filename Type Size Used Priority\n'
        active = empty + '/swap.img file 1048576 262144 -1\n'
        self.assertEqual(prepare.swap_paths(empty), [])
        self.assertEqual(prepare.swap_paths(active), ['/swap.img'])
        prepare.swap_headroom('MemAvailable: 1048576 kB', active)
        with self.assertRaises(ValueError):
            prepare.swap_headroom('MemAvailable: 524288 kB', active)
        with self.assertRaises(ValueError):
            prepare.swap_paths('invalid')

    def test_mount_unit_never_stores_key(self):
        unit = prepare.unit_text(1001, 1001)
        self.assertIn('noswap', unit)
        self.assertIn('uid=1001,gid=1001', unit)
        self.assertNotIn('database.key', unit)

    def test_wrong_key_preserves_previous_and_removes_candidate(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = pathlib.Path(folder)
            previous = directory / 'database.key'
            previous.write_bytes(b'previous-private-key')
            with patch.object(unlock.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)):
                with self.assertRaises(ValueError):
                    unlock.install_key(b'a' * 64, directory, pathlib.Path('/fixture.db'), pathlib.Path('/tool'))
            self.assertEqual(previous.read_bytes(), b'previous-private-key')
            self.assertEqual(list(directory.glob('.candidate-*')), [])

    def test_validated_key_is_atomic_and_private(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = pathlib.Path(folder)
            with patch.object(unlock.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)) as run:
                unlock.install_key(b'a' * 64, directory, pathlib.Path('/fixture.db'), pathlib.Path('/tool'))
            self.assertEqual((directory / 'database.key').read_bytes(), b'a' * 64 + b'\n')
            self.assertEqual((directory / 'database.key').stat().st_mode & 0o777, 0o600)
            self.assertEqual(run.call_args.kwargs['env']['GNOTES_VOLATILE_KEY'], '1')
            self.assertNotIn('a' * 64, str(run.call_args.args))

    def test_invalid_input_never_creates_candidate(self):
        with tempfile.TemporaryDirectory() as folder:
            for key in (b'', b'a' * 63, b'z' * 64, b'a' * 131):
                with self.assertRaises(ValueError):
                    unlock.install_key(key, pathlib.Path(folder))
            self.assertEqual(list(pathlib.Path(folder).iterdir()), [])

    def test_initial_staging_is_limited_to_plaintext_and_never_overwrites(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = pathlib.Path(folder)
            database = directory / 'fixture.db'
            database.write_bytes(b'encrypted-header')
            with self.assertRaises(ValueError):
                unlock.install_key(b'a' * 64, directory, database, stage=True)
            database.write_bytes(b'SQLite format 3\0')
            unlock.install_key(b'a' * 64, directory, database, stage=True)
            with self.assertRaises(ValueError):
                unlock.install_key(b'b' * 64, directory, database, stage=True)
            self.assertEqual((directory / 'database.key').read_bytes(), b'a' * 64 + b'\n')


if __name__ == '__main__':
    unittest.main()
