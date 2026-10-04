---
tags: [concept, queues]
aliases: [Flow control]
---
# Backpressure

When a consumer cannot keep up, something has to give. Backpressure is the
choice of *what* gives, made in advance instead of during an incident.

## Three honest options

1. **Buffer** — accept the work and queue it. Cheap until the buffer is
   full; then latency becomes unbounded.
2. **Drop** — refuse or discard work, and say so. Painful, but the
   pain is visible and bounded.
3. **Slow the producer** — make the caller wait. Correct, but it moves the
   problem upstream, where it may be worse.

Most systems pick *buffer* by accident and discover *drop* during an
outage. A queue is a buffer with a nice name.

## Budgets, not counts

A retry count ("try five times") multiplies load exactly when the system is
weakest. A retry **budget** caps retries as a fraction of total traffic:

$$
\text{retries allowed} = 0.1 \times \text{requests in the last minute}
$$

When the budget is spent, failures fail fast. For [[Lantern migration]] we
settled on a per-message cap of 5 plus a per-tenant budget, so one noisy
tenant cannot starve the rest.

## Signals

| Signal | Means | Act when |
|:--|:--|:--|
| Queue depth | work waiting | it grows for 10 min straight |
| Oldest message age | how stale the worst job is | above the job's SLO |
| Consumer lag | distance behind producers | above 5 min |
| Retry rate | how much work is repeated | above 10% of traffic |

The age of the oldest message is the one users feel; depth alone hides a
single stuck job behind a thousand fast ones.

See also [[Idempotency keys]] — retries are only safe when repeats are
harmless — and the feedback chapter of [[The Quiet Machine#Part II — Feedback]].
