---
id: note-20260716-dev-docs-v2-restructure
kind: note
title: dev-docs v2 documentation restructure
status: published
created: '2026-07-16'
topic: documentation-migration
summary: Consolidated governing architecture into dev-docs v2 while retaining README
  files as operational contracts.
tags:
- dev-docs
- migration
owners: []
relations: []
source_paths: []
updated: '2026-07-16'
---

## Summary

Introduced a v2 docs root and split the former monolithic architecture document into credential broker, provider boundary, and daemon/hook runtime designs.

## Notes

- `ARCHITECTURE.md` was retired after its durable content was consolidated into active designs and accepted decisions.
- Root and provider README files remain the canonical usage, configuration, and protocol guides.
- The pre-existing untracked `.mcp.json` was not modified.
