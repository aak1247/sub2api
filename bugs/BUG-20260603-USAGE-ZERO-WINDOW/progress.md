# BUG-20260603-USAGE-ZERO-WINDOW - Empty usage window displays stale utilization

- Status: `ARCHIVED`
- Scope: `BACKEND`
- Severity/Priority: `S2` / `P2`
- Assignee: `codex`
- Updated At: `2026-06-03T11:38:45Z`

## Reproduction
- Attempts/Successes: `1/1`
- Probability: `100.0%`

### Preconditions
- OpenAI OAuth account has codex usage snapshot
- window stats for 5h/7d are all zero

### Steps
- load account usage API
- render account usage progress bar

- Expected: zero request/token/cost window is shown as 0% and available now
- Actual: stale codex utilization such as 99% is returned/displayed

## Tracking
- Next Check Date: `2026-06-03`
- Blockers: None

## Timeline
- [2026-06-03T11:38:31Z] aak1247 | REPRODUCED | Bug intake completed and saved | Capture bug basics and reproduction signals
- [2026-06-03T11:38:38Z] aak1247 | RESOLVED | Fix landed in backend normalization and frontend defensive display; targeted tests passed. | Backend zeroes stale Codex utilization when account window stats are empty; frontend UsageProgressBar applies the same defensive rule. | next_check=2026-06-03
- [2026-06-03T11:38:45Z] aak1247 | ARCHIVED | Archive bug with automated regression test
