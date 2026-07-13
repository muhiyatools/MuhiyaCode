---
name: go-test-writer
description: Write idiomatic Go table-driven tests for a given package or function, including edge cases and error paths.
---

# Go Test Writer

When asked to write or add Go tests:
- Use table-driven subtests with `t.Run`.
- Cover the happy path, boundary values, and error returns.
- Name tests `TestXxx` matching the function under test.
- Prefer `t.Fatalf` with the got/want values in the message.
