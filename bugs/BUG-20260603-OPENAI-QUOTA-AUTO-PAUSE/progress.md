# BUG-20260603-OPENAI-QUOTA-AUTO-PAUSE - OpenAI quota auto-pause only filtered transiently and did not visibly pause accounts

- Status: `ARCHIVED`
- Scope: `BACKEND`
- Severity/Priority: `S2` / `P2`
- Assignee: ``
- Updated At: `2026-06-03T10:48:24Z`

## Reproduction
- Attempts/Successes: `1/1`
- Probability: `100.0%`

### Preconditions
- OpenAI account is active and schedulable
- Codex 5h used percent exceeds configured auto-pause threshold

### Steps
- select an OpenAI account for a request

- Expected: account is not selected and is persisted as rate-limited until the quota window reset time, without setting a permanent error
- Actual: account was only skipped transiently; account remained schedulable in persistent state

## Tracking
- Next Check Date: ``
- Blockers: None

## Timeline
- [2026-06-03T10:38:17Z] aak1247 | REPRODUCED | Bug intake completed and saved | Capture bug basics and reproduction signals
- [2026-06-03T10:38:17Z] aak1247 | ARCHIVED | Archive bug with automated regression test
- [2026-06-03T10:48:24Z] aak1247 | ARCHIVED | Correct archive semantics after product clarification | Quota threshold auto-pause now follows the existing 429 rate-limit flow via rate_limit_reset_at instead of SetError, so scheduling resumes after the reset window.
