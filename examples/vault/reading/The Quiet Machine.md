---
title: The Quiet Machine
author: Mara Okonkwo-Lind
tags: [reading, operability]
aliases: [Quiet Machine]
started: 2026-09-20
rating: 4/5
---
# The Quiet Machine

*Notes on operability*, a short book about building systems that are calm
to run. (A fictional book, written for this example vault.) Chapters are
short and end with a question, which I have copied below each section.

> [!abstract] In one sentence
> A system is operable when the people running it can tell what it is
> doing, change what it is doing, and stop it — quickly and without fear.

## Part I — Legibility

The first part argues that most incidents are *comprehension* failures
before they are technical ones. The system was telling us; we could not
read it.

> The dashboard that shows everything shows nothing. Choose the three
> numbers that would wake you, and put them where your eyes already are.

My notes:

- The "three numbers" for a queue are depth, age of the oldest message and
  consumer lag. That is exactly what we built for [[Lantern migration]].
- Logs are for *why*, metrics for *whether*. Mixing them up is how you end
  up grepping during an outage.
- Legibility includes names: a queue called `misc` is a future incident.

> [!question] Chapter question
> Which of your services could you explain to a new teammate in five
> minutes, using only what it reports about itself?

## Part II — Feedback

The middle of the book is about loops: retries, autoscalers, alerts. Every
loop needs a limit, or it becomes an amplifier.

> A retry is a promise to do the same work again, later, when things may
> be worse. Make that promise sparingly.

- This is the argument for a retry *budget* — see [[Backpressure]].
- Alert on symptoms users feel, page on symptoms that will get worse
  without a human. Everything else is a ticket.
- An autoscaler that scales on queue depth will happily scale a poisoned
  queue. Scale on throughput, cap on cost.

> [!example] A loop with a limit
> Our webhook retries used to back off forever. With a budget of five
> attempts over an hour, failures now land in a dead-letter queue that a
> person reads every morning.

## Part III — Control

Short and practical: feature flags, kill switches, and the discipline of
rehearsing the undo.

> If you have never pressed the button, you do not have a button.

- [x] Add a rehearsal step to the [[Lantern cutover]] runbook
- [ ] Find our three oldest kill switches and test them due:2026-10-23
- [ ] Lend the book to Kenji

## What I disagree with

The author dismisses runbooks as "documentation of failure". I think a good
runbook is documentation of *judgment*: it says when to stop. See the
"Stop if" box at the top of [[Lantern cutover]].

#reading/2026
