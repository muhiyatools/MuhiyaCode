---
name: sql-optimizer
description: Analyze a slow SQL query and propose indexes, rewrites, and EXPLAIN-guided improvements for Postgres.
---

# SQL Optimizer

Given a slow query:
- Read the schema and existing indexes first.
- Propose the smallest index or rewrite that removes the bottleneck.
- Explain the EXPLAIN plan change you expect.
