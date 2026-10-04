---
title: On-call
tags: [oncall, area]
aliases: [Pager, On call]
---
# On-call

One week at a time, Monday to Monday, handed over at 11:00. The primary
carries the pager; the secondary is the second pair of eyes and the person
who writes things down.

## The first five minutes

1. **Acknowledge** the page, so nobody else starts in parallel.
2. **Look** at the three numbers for the affected service before reading
   any logs.
3. **Say** in the incident channel what you see, even if it is "no idea
   yet". Silence is the scariest status.
4. **Decide** whether this needs a second person. If you are unsure, it
   does.

> [!tip] Rule of thumb
> If the fix is not obvious in fifteen minutes, roll back the last change
> and debug afterwards. A rollback is not an admission of anything.

## Rotation

| Week of | Primary | Secondary |
|:--|:--|:--|
| 28 Sep | Kenji | me |
| 5 Oct | me | Ines |
| 12 Oct | Ines | Tomás — *cutover week* |
| 19 Oct | Tomás | Priya |

## Handover

Write a short note in the daily note of the handover day:

- incidents and their state, with links;
- anything noisy that should be silenced or fixed;
- changes planned for the coming week (for October: [[Lantern cutover]]).

## Checklist for my week

- [ ] Test the pager on Monday morning due:2026-10-05
- [ ] Re-read [[Lantern cutover#Rollback]] before the rehearsal
- [ ] Silence the flapping disk alert on the old broker 📅 2026-10-03
- [x] Update the escalation contacts

#oncall
