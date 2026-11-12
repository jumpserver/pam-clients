import io
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zipfile
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import distribution


class DistributionTests(unittest.TestCase):
    def test_sources_are_deterministic_and_exclude_local_identity_and_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            sdk = root / 'python/jms_pam'
            sdk.mkdir(parents=True)
            module = sdk / '__init__.py'
            module.write_text('class Client: pass\n')
            identity = root / 'python/jms_pam_config.py'
            identity.write_text('app_secret = "do-not-distribute"')
            (sdk / 'linked.py').symlink_to(identity)
            before = distribution.source_archive('python', root)
            os.utime(module, (1000000000, 1000000000))
            after = distribution.source_archive('python', root)
            self.assertEqual(before, after)
            with tarfile.open(fileobj=io.BytesIO(after)) as archive:
                self.assertEqual(archive.getnames(), ['jms-pam-clients/python/jms_pam/__init__.py'])
            with zipfile.ZipFile(io.BytesIO(distribution.python_sdk_zip(root))) as archive:
                self.assertEqual(archive.namelist(), ['jms_pam/__init__.py'])

    def test_failed_build_does_not_publish_partial_or_overwrite_existing_version(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            with patch.object(distribution.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'go')):
                with self.assertRaises(subprocess.CalledProcessError):
                    distribution.build(output, ('linux/amd64',))
            self.assertEqual(list(output.iterdir()), [])
            version = output / distribution.client_version()
            version.mkdir()
            sentinel = version / 'previous-build'
            sentinel.write_text('keep')
            with self.assertRaises(FileExistsError):
                distribution.build(output, ('linux/amd64',))
            self.assertEqual(sentinel.read_text(), 'keep')


if __name__ == '__main__':
    unittest.main()
