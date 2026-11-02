---
type: question
created: 2026-11-01
status: open
kind: decision
scope: "[[acme-portal]]"
options:
  - /etc/atlas
  - /opt/atlas
recommended: /etc/atlas
tags: []
---

# Which config path does production use?

What is unknown, and what waits on the answer. A question is not work: a task is work to do. `scope` is one project, task, or area link, or the word `root` for the whole vault. `kind` is `question`, `decision` (answering needs a reason), or `input`. Leave out `options` and `recommended` when the answer is free text.

Ask it with `para question add`. Make a task wait on it with `para task depends add <task> <question>`. Answer with `para answer <id>`; the answer is recorded under `## Answer` and the status becomes `answered`.

## Answer

Recorded by `para answer`: the text, the choice, the reason, the source, who, and when. Never a secret: write where it is kept.
