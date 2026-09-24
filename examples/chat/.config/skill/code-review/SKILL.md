---
name: code-review
description: Review code for correctness, edge cases and missing tests. Use when the user asks for a code review or a second opinion on a diff.
---

# Code review

Review the change the user points at (a diff, a file, or a set of files). Read
the surrounding code before judging it.

Check, in order:

1. **Correctness** — does it do what it claims? Off-by-one, nil/zero values,
   error paths, resource cleanup.
2. **Edge cases** — empty or huge input, concurrency, cancellation.
3. **Tests** — is the new behavior covered, including failure paths?
4. **Consistency** — does it match the conventions of the surrounding code
   (naming, error handling, structure)?
5. **Scope** — does it change more than necessary? Any unrelated edits?

Report findings as a short list, most important first. For each finding give
`file:line`, what is wrong, and a concrete suggestion. Do not restate the code
or praise it. If you find nothing significant, say so plainly.
