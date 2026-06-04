# BUG-20260603-OPENAI-ZERO-QUOTA-AUTOPAUSE - OpenAI zero quota snapshot triggers quota auto-pause

- Status: `ARCHIVED`
- Scope: `BACKEND`
- Severity/Priority: `S2` / `P2`
- Assignee: `codex`
- Updated At: `2026-06-03T14:47:36Z`

## Reproduction
- Attempts/Successes: `1/1`
- Probability: `100.0%`

### Preconditions
- OpenAI quota auto-pause threshold is enabled
- Codex quota probe/header snapshot contains primary and secondary used/reset/window values all zero

### Steps
- persist Codex quota snapshot
- select an OpenAI account with auto-pause defaults enabled

- Expected: empty all-zero quota snapshot is treated as unknown/no usable signal and does not set temporary 429 unschedulable state
- Actual: 5h raw zero is normalized to 100% used, crosses threshold, and SetRateLimited/temporary 429 unschedulable state is applied

## Tracking
- Next Check Date: `2026-06-03`
- Blockers: None

## Timeline
- [2026-06-03T14:44:50Z] aak1247 | REPRODUCED | Bug intake completed and saved | Capture bug basics and reproduction signals
- [2026-06-03T14:45:01Z] aak1247 | RESOLVED | Skip canonical Codex used_percent updates for all-zero empty quota slots and verify auto-pause no longer triggers. | The 5h header can be remaining percent, but 0/0/0 percent-reset-window is an empty snapshot, not an exhausted window. | next_check=2026-06-03
- [2026-06-03T14:45:14Z] aak1247 | ARCHIVED | Archive bug with automated regression test
- [2026-06-03T14:47:28Z] aak1247 | RESOLVED | Add read-side guard so existing bad canonical zero-quota snapshots do not trigger auto-pause. | Regression now covers both new all-zero snapshots and previously persisted codex_5h/7d_used_percent=100 derived from empty raw quota slots. | next_check=2026-06-03
- [2026-06-03T14:47:36Z] aak1247 | ARCHIVED | Archive bug with automated regression test
