# BUG-20260601-ACCOUNT-DATA-IMPORT-WRAPPED - Account data JSON import rejects wrapped request payload as proxies is required

- Status: `ARCHIVED`
- Scope: `FRONTEND_PAGE`
- Severity/Priority: `S2` / `P2`
- Assignee: `codex`
- Updated At: `2026-06-01T09:48:07Z`

## Reproduction
- Attempts/Successes: `1/1`
- Probability: `100.0%`

### Preconditions
- admin opens account data import modal
- JSON file contains top-level data object wrapping sub2api-data payload

### Steps
- open admin accounts page
- open data import modal
- choose JSON file shaped as {data:{type,version,proxies,accounts},skip_default_group_bind:true}
- click import

### E2E Full Flow
- open admin accounts page
- open data import modal
- choose wrapped JSON export/request file
- submit import
- frontend sends inner data payload to /admin/accounts/data
- backend imports account without proxies is required error

- Expected: account import accepts wrapped JSON payload and imports accounts
- Actual: frontend wraps the uploaded JSON again so backend receives data.data and reports proxies is required

## Tracking
- Next Check Date: `2026-06-01`
- Blockers: None

## Timeline
- [2026-06-01T09:46:58Z] aak1247 | REPRODUCED | Bug intake completed and saved | Capture bug basics and reproduction signals
- [2026-06-01T09:47:25Z] aak1247 | RESOLVED | Verify account import accepts wrapped payload via frontend integration test and backend handler tests. | Fixed frontend payload normalization and narrowed backend payload validation by import surface. | next_check=2026-06-01
- [2026-06-01T09:48:07Z] aak1247 | ARCHIVED | Archive bug with automated regression test
