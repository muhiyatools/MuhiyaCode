// Package arch holds architecture-enforcement tests only (feature 010): a
// per-file size budget and the package-layering contract. It has no runtime
// code and imports only the standard library, so it sits at the foundation
// level and can never itself introduce a layering violation.
package arch
