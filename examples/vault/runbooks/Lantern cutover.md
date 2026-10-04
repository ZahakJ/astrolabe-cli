---
title: Lantern cutover
tags: [runbook, project/lantern]
aliases: [Cutover runbook]
reviewed: 2026-10-01
---
# Lantern cutover

Moves the consumers of one queue from Kestrel to Lantern. Run it once per
queue, smallest queue first. Expect 10–15 minutes per queue, most of it
waiting for Kestrel to drain.

> [!danger] Stop if
> - consumer lag on any Lantern queue is above **5 minutes**, or
> - the error rate on the queue's consumers doubles, or
> - anyone on the call asks you to.
>
> Stopping means [[#Rollback]], not debugging in production.

## Queues, in order

| Queue | Peak msg/s | Consumers | Owner | Notes |
|:--|--:|--:|:--|:--|
| `thumbnails` | 40 | 4 | media | safe to replay |
| `webhooks` | 120 | 6 | integrations | retries are visible to customers |
| `emails` | 310 | 8 | growth | dedupe by message id |
| `billing` | 85 | 3 | payments | **needs the FIFO fix first** |
| `search-events` | 900 | 12 | search | feeds [[Tidewater]] later |

## Preflight

Do these once, before the first queue.

- [ ] Clock skew under 100 ms on every consumer host
- [ ] Dashboards open: depth, oldest message age, consumer lag
- [ ] Dual write healthy for the last 24 h (zero publish errors)
- [ ] Rollback flag tested on staging this week

Check skew from the bastion:

```sh
# Prints each host's offset from the reference clock in milliseconds.
for h in $(lantern-hosts --role consumer); do
  printf '%-24s %s\n' "$h" "$(ssh "$h" chronyc tracking | awk '/System time/ {print $4 * 1000}')"
done
```

## Procedure

1. Announce in the incident channel: *"Starting Lantern cutover for
   `QUEUE`."*
2. Pause Kestrel consumers for the queue and wait for in-flight work:

   ```sh
   kestrelctl consumers pause --queue "$QUEUE"
   kestrelctl wait-idle --queue "$QUEUE" --timeout 10m
   ```

3. Flip the consumer flag. Lantern consumers start within 30 seconds:

   ```yaml
   # flags/lantern.yaml
   consumers:
     thumbnails: lantern   # was: kestrel
     webhooks: kestrel
   ```

4. Watch lag for five minutes. It should rise briefly, then fall to zero.
5. Drain what is left in Kestrel by replaying it into Lantern:

   ```sh
   lantern replay --from kestrel --queue "$QUEUE" --rate 200/s --dry-run
   lantern replay --from kestrel --queue "$QUEUE" --rate 200/s
   ```

6. Record the queue as done in [[Lantern migration#Log]].

## Rollback

The rollback is the flag flip in reverse. Lantern keeps messages for seven
days, so nothing is lost either way.

```diff
 consumers:
-  thumbnails: lantern
+  thumbnails: kestrel
```

Then resume the Kestrel consumers and replay anything Lantern accepted after
the flip:

```sql
-- Messages accepted by Lantern since the cutover started.
SELECT queue, count(*) AS pending, min(enqueued_at) AS oldest
FROM lantern_messages
WHERE queue = 'thumbnails'
  AND enqueued_at >= TIMESTAMP '2026-10-13 10:00:00'
  AND state = 'pending'
GROUP BY queue;
```

> [!tip] After a rollback
> Write down what you saw *before* trying again. The second attempt is
> never on the same day.

## After the last queue

- Turn off dual write: `lantern.dual_write = false`
- Leave Kestrel running, read-only, for seven days
- Hand over to on-call with a summary (see [[On-call#Handover]])
