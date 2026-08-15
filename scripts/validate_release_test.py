import tempfile
import unittest
from pathlib import Path

import validate_release


class ValidateReleaseTest(unittest.TestCase):
    def validate(self, manifest: str, tag: str) -> str:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "herdr-plugin.toml")
            path.write_text(manifest)
            return validate_release.validate(path, tag)

    def test_valid_versions(self):
        for version in ("0.3.0", "12.34.56"):
            with self.subTest(version=version):
                self.assertEqual(self.validate(f'version = "{version}"\n', "v" + version), version)

    def test_rejects_malformed_and_leading_zero_versions(self):
        for version in ("1.2", "1.2.3-rc.1", "01.2.3"):
            with self.subTest(version=version):
                with self.assertRaisesRegex(ValueError, "strict X.Y.Z"):
                    self.validate(f'version = "{version}"\n', "v" + version)

    def test_rejects_mismatched_tag(self):
        with self.assertRaisesRegex(ValueError, "must equal"):
            self.validate('version = "0.3.0"\n', "v0.3.1")

    def test_requires_top_level_version(self):
        with self.assertRaisesRegex(ValueError, "top-level version"):
            self.validate('[plugin]\nversion = "0.3.0"\n', "v0.3.0")

    def test_ignores_version_text_in_multiline_string(self):
        manifest = '''description = """
version = "0.3.0"
"""
'''
        with self.assertRaisesRegex(ValueError, "top-level version"):
            self.validate(manifest, "v0.3.0")


if __name__ == "__main__":
    unittest.main()
