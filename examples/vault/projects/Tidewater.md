---
title: Tidewater
tags: [project/tidewater, search]
status: paused
---
# Tidewater

Rebuild the search index incrementally instead of nightly. Paused until
[[Lantern migration|Lantern]] ships, because the indexer will consume change
events from a Lantern queue.

## Shape

- A change-data stream feeds an **indexer** that batches updates per
  document, so a burst of edits costs one write.
- Full rebuilds stay possible but become a tool, not a schedule.
- Queries never wait for indexing; freshness is reported, not promised.

## Numbers to beat

| Metric | Nightly today | Target |
|:--|--:|--:|
| Median freshness | 14 h | 30 s |
| p99 freshness | 26 h | 5 min |
| Rebuild time | 3 h 40 min | 3 h 40 min |
| Index size | 48 GB | 52 GB |

## Notes

The indexer must tolerate duplicates and reordering: each update carries the
document version and older versions are dropped. That is the same trick as
[[Idempotency keys]], applied to documents.

- [ ] Write the one-page proposal once Lantern is live 📅 2026-10-30
- [ ] Ask Priya about spare nodes for the reindex

#search #paused
