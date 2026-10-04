---
tags: [meeting]
date: 2026-10-02
---
# Platform sync — 2 October

Weekly, 30 minutes. Present: the platform team, plus Amara from support.

## Lantern

- Dual write has run for 36 hours with zero publish errors.
- Shadow consumers reported 212 mismatches on `emails`. All of them came
  from one consumer host whose clock was 4 s fast; the dedupe window
  treated retries as new messages. Fixed by syncing the host, not by
  changing [[Lantern migration|Lantern]].
- The rollback dry run moves to Friday 9 October.

> [!note] Clock skew is a dependency
> Anything that compares timestamps across hosts depends on time sync. Add
> a skew check to the cutover preflight in [[Lantern cutover#Preflight]].

## Support

Amara: three customer tickets this month were really queue backlogs that
looked like "emails not arriving". She would like a status page entry for
delayed jobs.

- [ ] Add "background jobs delayed" to the status page components due:2026-10-09
- [ ] Share the dashboard link with support

## Next week

On-call handover moves to Monday because of the cutover; see [[On-call]].
