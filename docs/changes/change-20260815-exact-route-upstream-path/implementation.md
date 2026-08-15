---
change: change-20260815-exact-route-upstream-path
role: implementation
---

<!-- lifecycle is owned by change.md -->

# Implementation

## Implementation

- exact patternとslash付きsubtree patternを同じprotected handlerへ登録する。
- `ReverseProxy.SetURL`後、stripped pathがemptyだった場合だけupstream Path/RawPathを戻す。
- exact POSTがupstreamへ末尾slashなしで届く回帰testを追加する。
