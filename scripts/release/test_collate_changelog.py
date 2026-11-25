"""Tests for collate_changelog.py. Run: python3 -m unittest discover -s scripts/release"""

import contextlib
import io
import sys
import tempfile
import textwrap
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import collate_changelog  # noqa: E402

CHANGELOG = """\
# Changelog

## Unreleased

### Breaking

- None for this release

### Changes

- CLI
  - Existing CLI entry.

## [v0.1.0](https://example.com) - 2026-01-01

### Changes

- CLI
  - Old entry.
"""


def dedent(text):
    return textwrap.dedent(text).lstrip("\n")


class CollateTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        (self.root / "changelog.d").mkdir()
        (self.root / "changelog.d" / "README.md").write_text("# not a fragment\n")
        self.write_changelog(CHANGELOG)

    def write_changelog(self, text):
        (self.root / "CHANGELOG.md").write_text(text)

    def changelog(self):
        return (self.root / "CHANGELOG.md").read_text()

    def fragment(self, name, text):
        (self.root / "changelog.d" / name).write_text(dedent(text))

    def fragments(self):
        return sorted(p.name for p in (self.root / "changelog.d").iterdir())

    def run_main(self, *args):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = collate_changelog.main(["--root", str(self.root), *args])
        return code, out.getvalue(), err.getvalue()

    def unreleased(self):
        text = self.changelog()
        return text[text.index("## Unreleased") : text.index("## [v0.1.0]")]

    def test_no_fragments_changes_nothing(self):
        self.assertEqual(self.run_main()[0], 0)
        self.assertEqual(self.changelog(), CHANGELOG)
        self.assertEqual(self.fragments(), ["README.md"])

    def test_fragments_for_one_component_merge_in_filename_order(self):
        self.fragment("b-branch.md", "### Changes\n\n- CLI\n  - Entry from b.\n")
        self.fragment("a-branch.md", "### Changes\n\n- CLI\n  - Entry from a.\n    continued.\n")
        self.assertEqual(self.run_main()[0], 0)
        self.assertEqual(
            self.unreleased(),
            dedent("""
                ## Unreleased

                ### Breaking

                - None for this release

                ### Changes

                - CLI
                  - Existing CLI entry.
                  - Entry from a.
                    continued.
                  - Entry from b.

            """),
        )
        self.assertEqual(self.fragments(), ["README.md"])

    def test_breaking_and_changes_go_to_their_sections_and_placeholder_drops(self):
        self.fragment("sdk.md", "### Breaking\n\n- SDK\n  - Breaking SDK entry.\n")
        self.fragment("tel.md", "### Changes\n\n- Telemetry\n  - Telemetry entry.\n")
        self.assertEqual(self.run_main()[0], 0)
        self.assertEqual(
            self.unreleased(),
            dedent("""
                ## Unreleased

                ### Breaking

                - SDK
                  - Breaking SDK entry.

                ### Changes

                - CLI
                  - Existing CLI entry.
                - Telemetry
                  - Telemetry entry.

            """),
        )
        self.assertIn("  - Old entry.\n", self.changelog())

    def test_component_names_merge_case_insensitively_keeping_first_spelling(self):
        self.write_changelog(CHANGELOG.replace("- CLI\n  - Existing", "- Onchain programs\n  - Existing", 1))
        self.fragment("x.md", "### Changes\n\n- Onchain Programs\n  - New entry.\n")
        self.assertEqual(self.run_main()[0], 0)
        self.assertIn("- Onchain programs\n  - Existing CLI entry.\n  - New entry.\n\n## [v0.1.0]", self.changelog())
        self.assertNotIn("Onchain Programs", self.changelog())

    def test_one_fragment_may_hold_both_sections(self):
        self.fragment("x.md", "### Breaking\n\n- SDK\n  - B.\n\n### Changes\n\n- SDK\n  - C.\n")
        self.assertEqual(self.run_main()[0], 0)
        self.assertIn("### Breaking\n\n- SDK\n  - B.\n\n### Changes\n\n- CLI\n  - Existing CLI entry.\n- SDK\n  - C.\n", self.changelog())

    def test_malformed_fragments_write_and_delete_nothing(self):
        cases = {
            "unknown heading": (
                "### Fixed\n\n- CLI\n  - x\n",
                "x.md:1: unknown heading '### Fixed', use one of: ### Breaking, ### Changes",
            ),
            "entry before component": ("### Changes\n\n  - x\n", "x.md:3: entry '  - x' has no '- Component' line above it"),
            "component before heading": ("- CLI\n  - x\n", "x.md:1: component '- CLI' comes before ### Breaking or ### Changes"),
            "component without entries": ("### Changes\n\n- CLI\n- SDK\n  - x\n", "x.md:3: component has no entries"),
            "empty file": ("", "x.md: has no entries"),
            "stray prose": ("### Changes\n\nsome text\n", "x.md:3: unexpected line 'some text'"),
            "already released": ("### Changes\n\n- CLI\n  - Old entry.\n", "x.md:4: entry is already in a released section, so delete this fragment"),
        }
        for name, (text, message) in cases.items():
            with self.subTest(name):
                self.fragment("a-good.md", "### Changes\n\n- CLI\n  - Good.\n")
                (self.root / "changelog.d" / "x.md").write_text(text)
                code, _, err = self.run_main()
                self.assertEqual(code, 1)
                self.assertEqual(err, f"error: changelog.d/{message}\n")
                self.assertEqual(self.changelog(), CHANGELOG)
                self.assertEqual(self.fragments(), ["README.md", "a-good.md", "x.md"])

    def test_symlinked_fragment_is_refused(self):
        (self.root / "secret").write_text("token\n")
        (self.root / "changelog.d" / "a.md").symlink_to(self.root / "secret")
        code, _, err = self.run_main()
        self.assertEqual(code, 1)
        self.assertEqual(err, "error: changelog.d/a.md: is not a regular file\n")
        self.assertEqual(self.changelog(), CHANGELOG)

    def test_unreleased_errors_name_the_changelog_line(self):
        self.write_changelog(CHANGELOG.replace("  - Existing CLI entry.\n", "  - Existing CLI entry.\nstray\n", 1))
        code, _, err = self.run_main("--check")
        self.assertEqual((code, err), (1, "error: CHANGELOG.md:13: unexpected line 'stray'\n"))

    def test_missing_unreleased_writes_and_deletes_nothing(self):
        text = CHANGELOG.replace("## Unreleased", "## Next")
        self.write_changelog(text)
        self.fragment("a.md", "### Changes\n\n- CLI\n  - Good.\n")
        code, _, err = self.run_main()
        self.assertEqual(code, 1)
        self.assertEqual(err, "error: CHANGELOG.md: expected one '## Unreleased' line, found 0\n")
        self.assertEqual(self.changelog(), text)
        self.assertEqual(self.fragments(), ["README.md", "a.md"])

    def test_check_and_dry_run_change_nothing(self):
        self.fragment("a.md", "### Changes\n\n- CLI\n  - New.\n")
        code, out, _ = self.run_main("--check")
        self.assertEqual((code, out), (0, "1 changelog fragment(s) parse cleanly\n"))
        code, out, _ = self.run_main("--dry-run")
        self.assertEqual(code, 0)
        self.assertIn("- CLI\n  - Existing CLI entry.\n  - New.\n", out)
        self.assertEqual(self.changelog(), CHANGELOG)
        self.assertEqual(self.fragments(), ["README.md", "a.md"])


if __name__ == "__main__":
    unittest.main()
