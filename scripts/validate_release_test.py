import tempfile
import unittest
from pathlib import Path

import validate_release


class ValidateReleaseTest(unittest.TestCase):
    def validate(self, manifest: str, tag: str, version_go: str | None = None) -> str:
        """Validate manifest against tag.

        version_go is the internal/version/version.go text. None writes a file
        that matches the tag. An empty string writes no file.
        """
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "herdr-plugin.toml")
            path.write_text(manifest)
            if version_go is None:
                version_go = f'const Current = "{tag.removeprefix("v")}"\n'
            if version_go:
                source = Path(directory, "internal", "version")
                source.mkdir(parents=True)
                Path(source, "version.go").write_text(version_go)
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

    def test_rejects_mismatched_go_version(self):
        with self.assertRaisesRegex(ValueError, "internal/version/version.go defines"):
            self.validate('version = "0.3.0"\n', "v0.3.0", 'const Current = "0.3.1"\n')

    def test_requires_a_readable_go_version_file(self):
        with self.assertRaisesRegex(ValueError, "internal/version/version.go is not readable"):
            self.validate('version = "0.3.0"\n', "v0.3.0", "")

    def test_requires_the_go_version_constant(self):
        with self.assertRaisesRegex(ValueError, "must define const Current"):
            self.validate('version = "0.3.0"\n', "v0.3.0", "package version\n")

    def test_ignores_version_text_in_multiline_string(self):
        manifest = '''description = """
version = "0.3.0"
"""
'''
        with self.assertRaisesRegex(ValueError, "top-level version"):
            self.validate(manifest, "v0.3.0")


if __name__ == "__main__":
    unittest.main()
