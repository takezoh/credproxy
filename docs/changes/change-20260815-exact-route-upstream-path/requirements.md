---
change: change-20260815-exact-route-upstream-path
role: requirements
---

<!-- lifecycle is owned by change.md -->

# Requirements

## Requirements

- exact route requestはServeMux redirectを発生させない。
- configured upstreamがpathを持ち、stripped request pathがemptyなら、そのpathを
  byte-equivalentに保つ。
- subtree requestの既存prefix stripping semanticsは維持する。
