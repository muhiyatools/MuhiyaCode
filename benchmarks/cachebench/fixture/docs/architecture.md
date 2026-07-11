# Architecture

The command layer parses one operation and delegates storage and validation to the
`internal/taskboard` package. JSON is the compatibility boundary: field names and input order
must remain stable. Configuration is declarative and intentionally separate from task data.

Future changes should keep CLI formatting out of the storage package and add focused tests for
validation before expanding commands.
