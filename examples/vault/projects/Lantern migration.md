---
title: Lantern migration
aliases: [Lantern, Queue migration]
tags: [project/lantern, platform]
status: in progress
owner: platform team
date: 2026-09-14
---
# Lantern migration

Move every background job from the old broker (*Kestrel*, eleven years old,
one node we are afraid to reboot) onto **Lantern**, the new partitioned
queue. Producers switch first, consumers drain both, and the old broker is
switched off when its depth stays at zero for a week.

![[lantern-topology.svg]]

## Why now

- Kestrel's disk is 81% full and grows ~2% a week; the vendor no longer
  ships patches.
- One slow consumer stalls *everyone*: there is no per-queue isolation, so
  a backlog in `thumbnails` delays password-reset emails.
- We want ==retries with a budget== and dead-lettering we can inspect, not
  a log line that says `requeue`.

## Plan

1. **Dual write** — producers publish to both brokers behind the
   `lantern.dual_write` flag. Consumers still read Kestrel.
2. **Shadow consume** — Lantern consumers run with side effects disabled
   and compare outcomes; mismatches go to the `lantern-shadow` channel.
3. **Cutover** — flip consumers queue by queue, smallest first. The
   procedure is in [[Lantern cutover]].
4. **Drain and retire** — stop dual writes, watch Kestrel depth, archive its
   data, power it off.

### Open questions

- How much reordering can the billing consumers tolerate? See
  [[Idempotency keys#Ordering is a separate problem]].
- Do we size partitions by tenant or by queue? Draft numbers live in the
  capacity spreadsheet, which I still have to tidy up.

## Decisions

| Date | Decision | Why |
|:--|:--|:--|
| 2026-09-15 | Keep at-least-once delivery | consumers are already idempotent |
| 2026-09-22 | 32 partitions per queue | headroom for 4× traffic |
| 2026-09-28 | Cut over on a Tuesday | full team on hand, low traffic |
| 2026-10-02 | Retry budget of 5 per message | matches today's worst case |

## Tasks

- [x] Write the dual-write shim for the job library
- [x] Dashboards: depth, age of oldest message, consumer lag
- [/] Shadow-consume the `emails` queue for 72 hours
- [ ] Load test at 4× peak with partition rebalancing 📅 2026-10-06 ⏫
- [ ] Dry-run the rollback on staging 📅 2026-10-09
- [ ] Cutover rehearsal with on-call 📅 2026-10-12
- [ ] Production cutover 📅 2026-10-13 ⏫
- [ ] Archive Kestrel data to cold storage 📅 2026-10-27
- [-] ~~Migrate the cron runner too~~ (out of scope, see [[2026-09-28 Lantern kickoff#Out of scope]])

## Risks

> [!warning] Ordering
> Lantern only orders messages *within a partition*. Any consumer that
> assumed global FIFO must be found before cutover, not after.

> [!question]- Why not just upgrade Kestrel?
> The upgrade path goes through two major versions with incompatible disk
> formats, and still leaves us with one node. We would spend the same
> effort and keep the same failure modes.

## Log

- **2026-09-14** — kickoff scheduled; scope agreed in
  [[2026-09-28 Lantern kickoff]].
- **2026-10-01** — dual write enabled for all producers; no errors.
- **2026-10-02** — shadow mismatches traced to clock skew, not Lantern
  (details in [[2026-10-02 Platform sync]]).

Related: [[Backpressure]] · [[On-call]] · [[Tidewater]]
