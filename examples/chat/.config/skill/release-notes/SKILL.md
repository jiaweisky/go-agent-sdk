---
name: release-notes
description: Draft release notes from the git history. Use when the user asks for release notes, a changelog, or "what changed since the last release".
---

# Release notes

Turn the commit history into notes the user can paste into a release.

Steps:

1. Pick the range: the last tag (`git describe --tags --abbrev=0`) up to `HEAD`,
   or a range the user gives you.
2. Read the commits (`git log --oneline <range>`) and group them into Features,
   Fixes and Internal.
3. Write one line per user-visible change, in the user's language. Drop noise
   (formatting, dependency bumps unless notable).
4. Follow the structure in `template.md`, which sits next to this file.

If there are no tags, summarize the recent commits instead and say which range
you used.
