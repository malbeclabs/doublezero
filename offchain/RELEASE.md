# Release Guide

This is a quick guide for releasing packages in this repository using `release-plz`.

## TLDR

1. Click "Run workflow" in GitHub Actions
2. Wait for PR to appear
3. Review and merge the PR
4. Done - tags and binaries created automatically

---

## Example: Releasing a New Version

Let's say you want to release `sentinel v0.2.1` because you've made some bug fixes since `v0.2.0`.

### Current State

- Latest tag: `sentinel/v0.2.0`
- Current version in Cargo.toml: `0.2.0`
- You've made 5 commits since the last release

### What You Do (Step-by-Step)

#### Step 1: Trigger the Workflow

```
GitHub → Actions → "Release Please (release-plz)" → Run workflow
```

Click the button. That's it.

#### Step 2: Wait for PR

release-plz analyzes commits since `sentinel/v0.2.0` and opens a PR with these changes:

**File: `crates/sentinel/Cargo.toml`**

```diff
- version = "0.2.0"
+ version = "0.2.1"
```

**File: `crates/sentinel/CHANGELOG.md`**

```diff
+ ## [0.2.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/sentinel/v0.2.1) - 2025-10-22
+
+ ### Other
+ - fix thing 1
+ - improve thing 2
```

**File: `Cargo.lock`**

```diff
(dependency updates)
```

#### Step 3: Review the PR

Look at the PR. Check:

- ✅ Version number is correct (`0.2.1`)
- ✅ Changelog entries look good
- ✅ Only the crates you want to release are included

**Optional**: Edit the PR if needed:

- Want `0.3.0` instead? Edit `Cargo.toml` and `CHANGELOG.md`
- Want to improve changelog wording? Edit `CHANGELOG.md`
- Want to skip releasing a crate? Revert its changes

#### Step 4: Merge the PR (10 seconds)

Click "Merge pull request" → "Confirm merge"

#### Step 5: Wait for Automation

**Immediately after merge**

- ✅ Tag created: `sentinel/v0.2.1`
- ✅ Tag pushed to GitHub
- ✅ GitHub Release created with changelog

**Then** (5 minutes):

- ✅ Goreleaser builds binary
- ✅ Packages created (`.tar.gz`, `.deb`)
- ✅ Artifacts uploaded to GitHub release
- ✅ Slack notification sent

### Done!

The release should be live. Users can download `sentinel v0.2.1`.

---

## Multiple Crates in One Release

The PR can include multiple crates at once:

```
This PR releases:
- sentinel: 0.2.0 → 0.2.1
- solana-cli: 0.1.1 → 0.1.2
- contributor-rewards: 0.2.1-rc1 → 0.2.1
```

Each crate that has commits since its last tag will be included.

**If you only want to release some crates**:

1. Edit the PR to revert the crates you don't want
2. Merge the PR
3. Only the remaining crates get tagged

---

## Customizing Version Numbers

### Default: Patch Bump

By default, release-plz bumps the **patch** version:

- `0.2.0` → `0.2.1`
- `1.5.3` → `1.5.4`

### Option 1: Use Conventional Commits

Add prefixes to your commit messages:

```bash
git commit -m "feat: add new feature"         # → 0.2.0 → 0.3.0 (minor)
git commit -m "fix: fix bug"                  # → 0.2.0 → 0.2.1 (patch)
git commit -m "feat!: breaking change"        # → 0.2.0 → 1.0.0 (major)
```

Prefixes:

- `feat:` = minor bump (new feature)
- `fix:` = patch bump (bug fix)
- `feat!:` or `BREAKING CHANGE:` = major bump (breaking change)
- `docs:`, `chore:`, `test:` = no version bump

### Option 2: Edit the PR

Don't like the version release-plz chose?

1. Open the release PR
2. Edit `Cargo.toml`: Change `0.2.1` → `0.3.0`
3. Edit `CHANGELOG.md`: Change `## [0.2.1]` → `## [0.3.0]`
4. Commit to the PR
5. Merge

---

## FAQ

### Q: When should I trigger a release?

**A**: Whenever you want! Common patterns:

- After merging important features
- When bug fixes are ready for users
- Before major milestones
- Could do on some schedule but not needed for now

### Q: What if I don't want to release right now?

**A**: Just don't trigger the workflow. Commits accumulate on `main`, and when ready, trigger the workflow and all changes since the last tag will be in the release.

### Q: Can I release only one crate?

**A**: Yes. The PR will show all crates with changes. You can:

- Revert the ones you don't want in the PR before merging, OR
- Close the PR and manually bump versions + create tags

### Q: What if the automated PR looks wrong?

**A**:

1. Close the PR (don't merge)
2. Fix the issue manually
3. Trigger the workflow again, OR
4. Do a manual release (see below)

### Q: What if release-plz breaks?

**A**: You can always release manually:

```bash
# 1. Update version
vim crates/sentinel/Cargo.toml  # version = "0.2.1"

# 2. Update changelog
vim crates/sentinel/CHANGELOG.md

# 3. Update Cargo.lock
cargo check

# 4. Commit and push
git add -A
git commit -m "chore: release sentinel v0.2.1"
git push origin main

# 5. Create tag
git tag sentinel/v0.2.1
git push origin sentinel/v0.2.1

# Goreleaser will automatically build the binary
```

### Q: Where are the binaries?

**A**: GitHub Releases page for this repository. Each tag has a release with downloadable binaries.

### Q: How do I see what will be in the next release?

**A**: Check commits since the last tag:

```bash
git log sentinel/v0.2.0..HEAD -- crates/sentinel/
```

This shows all commits that will be in the changelog.

---

## Testing Locally

You can test release-plz locally before triggering the GitHub workflow.

### Prerequisites

Install release-plz:

```bash
cargo install --locked release-plz
```

### Preview What Will Be Released

See what release-plz would do without making any changes:

```bash
# This command analyzes commits and shows what would change
release-plz update --help
```

**Note**: There's no `--dry-run` flag, but you can run it on a branch and review the changes with `git diff`.

### Test on a Branch

```bash
# 1. Create a test branch
git checkout -b test-release-plz

# 2. Run release-plz
release-plz update

# 3. Review what changed
git status
git diff

# 4. Check specific files
cat crates/sentinel/CHANGELOG.md
cat crates/sentinel/Cargo.toml

# 5. If it looks good, you're ready to trigger the workflow
# If not, make adjustments to release-plz.toml

# 6. Clean up
git checkout main
git branch -D test-release-plz
```

### Common Local Commands

```bash
# Update changelogs and versions
release-plz update

# Update only a specific package
release-plz update --package doublezero-ledger-sentinel

# Check what commits will be included
git log sentinel/v0.2.0..HEAD -- crates/sentinel/

# Validate release-plz.toml syntax
release-plz generate-schema
# (Creates .schema/latest.json for IDE autocomplete)
```

### Troubleshooting Local Runs

**"Package not found in registry"**

- This is normal - our packages aren't published to crates.io
- release-plz will use git tags as the source of truth

**"No upstream configured for branch"**

- Warning only, safe to ignore
- Means your local branch doesn't track a remote

**"Cannot read package metadata"**

- Check that all Cargo.toml files are valid
- Run `cargo check` to verify

---

## Release Checklist

Before triggering:

- [ ] Tests are passing on `main`
- [ ] CI is green
- [ ] You've decided which crates need releases

When reviewing PR:

- [ ] Version numbers are correct
- [ ] Changelog entries are accurate
- [ ] No unexpected changes

After merging:

- [ ] Tags created successfully
- [ ] GitHub releases created
- [ ] Binaries built and uploaded

---
