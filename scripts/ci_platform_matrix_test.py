import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


class PlatformMatrixTest(unittest.TestCase):
    def test_command_validates_manifest_and_emits_matrix(self):
        script = Path(__file__).with_name("ci_platform_matrix.py")
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory, "herdr-plugin.toml")
            for data, expected_error in (
                ('platforms = ["macos", "linux", "windows"]', ""),
                ('platforms = ["freebsd"]', "unsupported manifest platform"),
                ('platforms = []', "nonempty platforms array"),
                ('name = "PR Board"', "nonempty platforms array"),
                ('platforms = "linux"', "nonempty platforms array"),
                ('platforms = [1]', "unsupported manifest platform"),
                ('platforms = [', "ci-platform-matrix:"),
            ):
                with self.subTest(manifest=data):
                    manifest.write_text(data, encoding="utf-8")
                    result = subprocess.run(
                        [sys.executable, "-B", str(script), str(manifest)],
                        capture_output=True, text=True, check=False,
                    )
                    if expected_error:
                        self.assertEqual(result.returncode, 1)
                        self.assertEqual(result.stdout, "")
                        self.assertIn(expected_error, result.stderr)
                    else:
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertEqual(result.stderr, "")
                        self.assertEqual(json.loads(result.stdout), {"include": [
                            {"platform": "macos", "goos": "darwin"},
                            {"platform": "linux", "goos": "linux"},
                            {"platform": "windows", "goos": "windows"},
                        ]})


if __name__ == "__main__":
    unittest.main()
