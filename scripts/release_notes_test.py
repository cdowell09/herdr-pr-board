import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


@unittest.skipUnless(shutil.which("git-cliff"), "git-cliff is required")
class ReleaseNotesTest(unittest.TestCase):
    def test_preview_matches_tag_and_preserves_older_release_format(self):
        config = Path(__file__).resolve().parent.parent / "cliff.toml"
        with tempfile.TemporaryDirectory() as directory:
            env = dict(os.environ, GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull)

            def run(*args):
                return subprocess.run(
                    args, cwd=directory, env=env, check=True, text=True, capture_output=True
                ).stdout

            run("git", "init", "--quiet")
            run("git", "config", "user.name", "Release test")
            run("git", "config", "user.email", "release@example.invalid")
            run("git", "-c", "core.hooksPath=" + os.devnull, "commit", "--allow-empty", "-m", "feat: older feature")
            run("git", "tag", "-a", "v0.1.0", "-m", "Release v0.1.0")
            run("git", "-c", "core.hooksPath=" + os.devnull, "commit", "--allow-empty", "-m", "feat: new feature")
            highlights = "A useful release.\n\n### Highlights\n\n- Review with more agents.\n"
            cliff = ("git-cliff", "--config", str(config), "--offline", "--strip", "header")
            preview = run(*cliff, "--unreleased", "--tag", "v0.2.0", "--with-tag-message", highlights)
            run("git", "tag", "-a", "v0.2.0", "--cleanup=verbatim", "-m", highlights)
            published = run(*cliff, "--current")
            self.assertEqual(preview, published)
            self.assertLess(published.index("### Highlights"), published.index("<details>"))
            self.assertLess(published.index("<details>"), published.index("New feature"))
            self.assertTrue(published.endswith("</details>\n"))
            self.assertNotIn("Older feature", published)
            full = run(*cliff)
            older = full.split("## 0.1.0", 1)[1]
            self.assertIn("Older feature", older)
            self.assertNotIn("<details>", older)
            self.assertEqual(full.count("### Highlights"), 1)


if __name__ == "__main__":
    unittest.main()
