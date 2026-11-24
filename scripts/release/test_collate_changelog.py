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

REPO = Path(__file__).resolve().parents[2]

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
            "unknown heading": ("### Fixed\n\n- CLI\n  - x\n", "unknown heading"),
            "entry before component": ("### Changes\n\n  - x\n", "has no '- Component'"),
            "component before heading": ("- CLI\n  - x\n", "comes before"),
            "component without entries": ("### Changes\n\n- CLI\n- SDK\n  - x\n", "x.md:3: component has no entries"),
            "empty file": ("", "has no entries"),
            "stray prose": ("### Changes\n\nsome text\n", "unexpected line"),
        }
        for name, (text, message) in cases.items():
            with self.subTest(name):
                self.fragment("a-good.md", "### Changes\n\n- CLI\n  - Good.\n")
                (self.root / "changelog.d" / "x.md").write_text(text)
                code, _, err = self.run_main()
                self.assertEqual(code, 1)
                self.assertIn(message, err)
                self.assertEqual(self.changelog(), CHANGELOG)
                self.assertEqual(self.fragments(), ["README.md", "a-good.md", "x.md"])

    def test_symlinked_fragment_is_refused(self):
        (self.root / "secret").write_text("token\n")
        (self.root / "changelog.d" / "a.md").symlink_to(self.root / "secret")
        code, _, err = self.run_main()
        self.assertEqual(code, 1)
        self.assertIn("changelog.d/a.md: is not a regular file", err)
        self.assertNotIn("token", err)
        self.assertEqual(self.changelog(), CHANGELOG)

    def test_missing_unreleased_writes_and_deletes_nothing(self):
        text = CHANGELOG.replace("## Unreleased", "## Next")
        self.write_changelog(text)
        self.fragment("a.md", "### Changes\n\n- CLI\n  - Good.\n")
        code, _, err = self.run_main()
        self.assertEqual(code, 1)
        self.assertIn("expected one '## Unreleased' line, found 0", err)
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

    def test_repo_unreleased_entries_survive_collation(self):
        # Blank lines may be normalized, so compare only the lines that carry text.
        lines = (REPO / "CHANGELOG.md").read_text().splitlines()
        start = lines.index("## Unreleased")
        end = next((i for i in range(start + 1, len(lines)) if lines[i].startswith("## ")), len(lines))
        merged = {name: collate_changelog.Section() for name in collate_changelog.SECTIONS}
        collate_changelog.parse(lines[start + 1 : end], "CHANGELOG.md", merged)
        rendered = [line for line in collate_changelog.render(merged) if line.strip()]
        self.assertEqual(rendered, [line for line in lines[start:end] if line.strip()])


if __name__ == "__main__":
    unittest.main()
