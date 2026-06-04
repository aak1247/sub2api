# BUG-20260604-OPENAI-429-RESET - OpenAI 429 cooldown ignores resets_in_seconds

- Status: `ARCHIVED`
- Scope: `BACKEND`
- Severity/Priority: `S2` / `P2`
- Assignee: `codex`
- Updated At: `2026-06-04T12:26:11Z`

## Reproduction
- Attempts/Successes: `1/1`
- Probability: `100.0%`

### Preconditions
- OpenAI upstream returns HTTP 429 with response body/header containing resets_in_seconds

### Steps
- send request through OpenAI gateway
- upstream returns 429
- inspect account/model cooldown reset time

- Expected: cooldown reset is derived from upstream resets_in_seconds when present
- Actual: cooldown reset often uses an incorrect fallback duration instead of resets_in_seconds

## Tracking
- Next Check Date: `2026-06-04`
- Blockers: None

## Timeline
- [2026-06-04T12:19:29Z] aak1247 | REPRODUCED | Bug intake completed and saved | Capture bug basics and reproduction signals
- [2026-06-04T12:20:30Z] aak1247 | IN_PROGRESS | Prioritize OpenAI 429 body resets_in_seconds over x-codex header fallback and add regression test | Root cause: handle429 checks x-codex headers before OpenAI body resets_in_seconds, so body-provided reset can be ignored. | next_check=2026-06-04
- [2026-06-04T12:26:11Z] aak1247 | RESOLVED | Verified OpenAI 429 reset uses response body resets_in_seconds before x-codex fallback | Fix: handle429 now parses OpenAI response body resets_at/resets_in_seconds before calculateOpenAI429ResetTime(headers). Targeted regression passed: cd backend && go test ./internal/service -run TestHandle429_OpenAIPrefersBodyResetsInSecondsOverCodexHeaders -count=1 | next_check=2026-06-04
- [2026-06-04T12:26:11Z] aak1247 | ARCHIVED | Archive bug with automated regression test
