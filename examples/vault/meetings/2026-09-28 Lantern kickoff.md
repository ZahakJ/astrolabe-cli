---
tags: [meeting, project/lantern]
date: 2026-09-28
attendees: Priya, Tomás, Ines, Kenji, me
---
# Lantern kickoff

**Goal:** agree scope and a cutover date for [[Lantern migration]].

## Notes

- Tomás walked through the Kestrel failure in August: one consumer leaked
  connections, the broker hit its file-descriptor limit and *every* queue
  stopped for 40 minutes.
- Ines: billing consumers assume FIFO in two places. Both can be fixed with
  a version check — see [[Idempotency keys]].
- Kenji wants the dashboards before any traffic moves. Agreed: no dashboard,
  no cutover.
- Rough agreement that a retry *budget* beats a retry *count*; Priya to
  write it up with numbers (see [[Backpressure#Budgets, not counts]]).

## Decisions

1. Cutover on **Tuesday 13 October**, 10:00, queue by queue.
2. At-least-once delivery stays; consumers must stay idempotent.
3. The rollback is a flag flip, rehearsed on staging first.

## Out of scope

- The cron runner. It uses Kestrel only for locking and can move later.
- Changing message formats. Payloads are copied byte for byte.

## Action items

- [x] Me — draft the [[Lantern cutover]] runbook 📅 2026-10-01
- [ ] Kenji — consumer-lag alert with a 5-minute threshold 📅 2026-10-05
- [ ] Ines — fix the two FIFO assumptions in billing 📅 2026-10-08
- [ ] Priya — retry budget proposal 📅 2026-10-07
