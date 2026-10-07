import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("prepare_apt", Path(__file__).with_name("prepare_apt.py"))
apt = importlib.util.module_from_spec(spec)
spec.loader.exec_module(apt)


class PackageMirrorTests(unittest.TestCase):
    def fixture(self, directory):
        root = Path(directory)
        config = root / "etc/apt/apt.conf.d"
        config.mkdir(parents=True)
        return root, config, root / "etc/apt/apt-mirrors.txt"

    def test_only_exact_azure_mirror_uri_is_changed(self):
        with tempfile.TemporaryDirectory() as directory:
            root, config, mirrors = self.fixture(directory)
            other = ("https://packages.microsoft.com/ubuntu/24.04/prod\n"
                     "http://azure.archive.ubuntu.com/ubuntu-extra\n"
                     "http://azure.archive.ubuntu.com.evil.invalid/ubuntu\n")
            mirrors.write_text("http://azure.archive.ubuntu.com/ubuntu priority:1\n"
                               "https://azure.archive.ubuntu.com/ubuntu/\n" + other)
            apt.prepare_apt(root)
            self.assertEqual(mirrors.read_text(), "https://archive.ubuntu.com/ubuntu priority:1\n"
                             "https://archive.ubuntu.com/ubuntu\n" + other)
            bounds = config / "99-whento-pr163-bounds"
            self.assertEqual(bounds.read_text(), apt.BOUNDS)
            self.assertEqual(bounds.stat().st_mode & 0o777, 0o644)

    def test_missing_mirror_list_is_not_created(self):
        with tempfile.TemporaryDirectory() as directory:
            root, config, mirrors = self.fixture(directory)
            apt.prepare_apt(root)
            self.assertFalse(mirrors.exists())
            self.assertEqual((config / "99-whento-pr163-bounds").read_text(), apt.BOUNDS)

    def test_setup_is_idempotent(self):
        with tempfile.TemporaryDirectory() as directory:
            root, _, mirrors = self.fixture(directory)
            mirrors.write_text("http://azure.archive.ubuntu.com/ubuntu\n")
            apt.prepare_apt(root)
            first = mirrors.read_text()
            apt.prepare_apt(root)
            self.assertEqual(mirrors.read_text(), first)

    def test_symlinked_files_are_not_followed(self):
        for filename in ("etc/apt/apt-mirrors.txt", "etc/apt/apt.conf.d/99-whento-pr163-bounds"):
            with self.subTest(filename=filename), tempfile.TemporaryDirectory() as directory:
                root, _, _ = self.fixture(directory)
                target = root / "sentinel"
                target.write_text("preserve")
                (root / filename).symlink_to(target)
                with self.assertRaisesRegex(RuntimeError, "symlinked"):
                    apt.prepare_apt(root)
                self.assertEqual(target.read_text(), "preserve")

    def test_missing_configuration_directory_fails_without_creating_it(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(RuntimeError):
                apt.prepare_apt(root)
            self.assertFalse((root / "etc").exists())

    def test_main_refuses_operator_upstream_and_unprivileged_execution(self):
        for environment, uid in (({}, 0), ({"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "When-To/whento"}, 0),
                                 ({"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "Akkitto/whento"}, 1000)):
            with self.subTest(environment=environment, uid=uid), \
                    patch.dict(os.environ, environment, clear=True), \
                    patch.object(apt.os, "geteuid", return_value=uid), patch.object(apt, "prepare_apt") as prepare:
                with self.assertRaises(RuntimeError):
                    apt.main()
                prepare.assert_not_called()


if __name__ == "__main__":
    unittest.main()
