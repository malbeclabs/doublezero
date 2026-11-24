#!/usr/bin/env python3
"""Fold changelog.d/*.md fragments into the "## Unreleased" section of CHANGELOG.md.

Each fragment holds the lines a PR would paste into "## Unreleased": a
"### Breaking" or "### Changes" heading, then "- Component" lines with
"  - entry" sub-bullets. bump-version.sh runs this before it promotes the
section. See changelog.d/README.md for the format.

Usage: collate_changelog.py [--root DIR] [--check | --dry-run]
"""

import argparse
import os
import sys
from pathlib import Path

SECTIONS = ("Breaking", "Changes")
PLACEHOLDER = "None for this release"
UNRELEASED = "## Unreleased"


class FragmentError(Exception):
    pass


class Section:
    def __init__(self):
        # Keyed by the lowercased component name, so "Onchain Programs" and
        # "Onchain programs" merge. The first spelling seen is kept.
        self.components = {}
        self.placeholder = False

    def component(self, name):
        key = name.lower()
        if key not in self.components:
            self.components[key] = (name, [])
        return self.components[key][1]


def parse(lines, source, changelog):
    """Parse lines into changelog, a dict of section name to Section.

    Return the number of entries found. Raise FragmentError on any line
    that does not fit the format.
    """
    section = None
    entries = None
    entry = None
    count = 0
    # The line of a component that has no entry yet in these lines.
    empty = None
    for number, line in enumerate(lines, 1):
        where = f"{source}:{number}"
        if not line.strip():
            continue
        if empty and not line.startswith("  - "):
            raise FragmentError(f"{empty}: component has no entries")
        if line.startswith("### "):
            name = line[4:].strip()
            if name not in SECTIONS:
                raise FragmentError(f"{where}: unknown heading {line!r}, use one of: ### Breaking, ### Changes")
            section = changelog[name]
            entries = entry = None
        elif line.startswith("- "):
            if section is None:
                raise FragmentError(f"{where}: component {line!r} comes before ### Breaking or ### Changes")
            name = line[2:].strip()
            if name.lower() == PLACEHOLDER.lower():
                section.placeholder = True
                entries = entry = None
                continue
            entries = section.component(name)
            entry = None
            empty = where
        elif line.startswith("  - "):
            if entries is None:
                raise FragmentError(f"{where}: entry {line!r} has no '- Component' line above it")
            entry = [line]
            entries.append(entry)
            empty = None
            count += 1
        elif line.startswith("    ") and entry is not None:
            entry.append(line)
        else:
            raise FragmentError(f"{where}: unexpected line {line!r}")
    if empty:
        raise FragmentError(f"{empty}: component has no entries")
    return count


def render(changelog):
    out = [UNRELEASED, ""]
    for name in SECTIONS:
        section = changelog[name]
        out += [f"### {name}", ""]
        if section.components:
            for component, entries in section.components.values():
                out.append(f"- {component}")
                for entry in entries:
                    out += entry
            out.append("")
        elif section.placeholder:
            out += [f"- {PLACEHOLDER}", ""]
    return out


def collate(root, mode):
    """Merge the fragments under root. mode is "check", "dry-run" or "write".

    Every file is parsed before anything is written, and fragments are deleted
    only after CHANGELOG.md is written.
    """
    changelog_path = root / "CHANGELOG.md"
    fragments = sorted(p for p in (root / "changelog.d").glob("*.md") if p.name != "README.md")
    lines = changelog_path.read_text().splitlines()

    starts = [i for i, line in enumerate(lines) if line == UNRELEASED]
    if len(starts) != 1:
        raise FragmentError(f"CHANGELOG.md: expected one '{UNRELEASED}' line, found {len(starts)}")
    start = starts[0]
    end = next((i for i in range(start + 1, len(lines)) if lines[i].startswith("## ")), len(lines))

    merged = {name: Section() for name in SECTIONS}
    parse(lines[start + 1 : end], "CHANGELOG.md", merged)
    for path in fragments:
        source = f"changelog.d/{path.name}"
        # A symlink could make the bump read, and print in an error, a file outside the repo.
        if path.is_symlink() or not path.is_file():
            raise FragmentError(f"{source}: is not a regular file")
        if parse(path.read_text().splitlines(), source, merged) == 0:
            raise FragmentError(f"{source}: has no entries")

    section = render(merged)
    if mode == "check":
        print(f"{len(fragments)} changelog fragment(s) parse cleanly")
        return
    if mode == "dry-run":
        print("\n".join(section))
        return
    if not fragments:
        return

    text = "\n".join(lines[:start] + section + lines[end:]) + "\n"
    tmp = changelog_path.with_suffix(".md.tmp")
    tmp.write_text(text)
    os.replace(tmp, changelog_path)
    for path in fragments:
        path.unlink()
    print(f"Collated {len(fragments)} changelog fragment(s) into {UNRELEASED}")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", type=Path, default=Path("."), help="repo root (default: .)")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check", action="store_const", const="check", dest="mode", help="parse only")
    mode.add_argument("--dry-run", action="store_const", const="dry-run", dest="mode", help="print the merged section")
    args = parser.parse_args(argv)
    try:
        collate(args.root, args.mode or "write")
    except FragmentError as err:
        print(f"error: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
