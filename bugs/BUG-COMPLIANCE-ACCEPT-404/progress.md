# BUG-COMPLIANCE-ACCEPT-404 - Admin compliance accept API returns 404

- Status: `READY_FOR_VERIFY`
- Scope: `API`
- Severity/Priority: `S2` / `P2`
- Assignee: `codex`
- Updated At: `2026-08-06T15:58:18Z`

## Reproduction
- Attempts/Successes: `1/1`
- Probability: `100.0%`

### Preconditions
- admin user is authenticated
- compliance accept flow is triggered

### Steps
- POST /api/v1/admin/compliance/accept

- Expected: API route exists and records/accepts compliance status
- Actual: 404 Not Found

## Tracking
- Next Check Date: `2026-08-06`
- Blockers: None

## Timeline
- [2026-08-06T15:51:06Z] aak1247 | REPRODUCED | Bug intake completed and saved | Capture bug basics and reproduction signals
- [2026-08-06T15:55:08Z] aak1247 | READY_FOR_VERIFY | Rebuild and push Docker image containing restored admin compliance routes | Restored /api/v1/admin/compliance and /api/v1/admin/compliance/accept route registration; added route coverage test; go build ./cmd/server passes. | next_check=2026-08-06
- [2026-08-06T15:58:18Z] aak1247 | READY_FOR_VERIFY | Deploy or restart production service with digest sha256:199aac0a87d85e56235cd03a040f996f39e94023b5b3449d3bc3884d0cd421f6, then re-probe POST /api/v1/admin/compliance/accept | Registry latest now points to fixed digest sha256:199aac0a87d85e56235cd03a040f996f39e94023b5b3449d3bc3884d0cd421f6. Direct production probe still returns 404, indicating the live instance has not picked up the new image. | next_check=2026-08-06
