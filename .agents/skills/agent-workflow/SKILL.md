---
name: agent-workflow
description: Rules for tracking work in Linear issues and for branches, pull requests and handoff reports. Use in a repository whose AGENTS.md or CLAUDE.md says to follow agent-workflow, before creating an issue, branch or PR.
---

# Agent Workflow with Linear

These rules apply only in a repository whose AGENTS.md or CLAUDE.md says to
follow them. A repository's own AGENTS.md or CLAUDE.md may add to them or
override them; when the two conflict, follow the repository.

## Values Read from the Repository

Read these values instead of asking for them or writing them down.

- `<owner>/<repo>`: the GitHub repository of the `origin` remote
  (`git remote get-url origin`).
- Default branch: the branch `origin/HEAD` points to
  (`git symbolic-ref refs/remotes/origin/HEAD`).
- Linear team: the team of existing issues that carry the repository's `Repo`
  label. When there are none, ask the person who asked. The team key is the
  prefix of the issue identifier, such as `ABC` in `ABC-123`.
- Rule files: AGENTS.md, CLAUDE.md, and the files they tell you to read.

The Linear workspace must have a `Repo` label group, the type labels
`Feature`, `Improvement` and `Bug`, and the statuses listed below. When any of
them is missing, ask the person who asked instead of creating it.

## Issue Tracking

Track work in Linear issues. Write in an issue only what will be needed for
later decisions, and do not repeat what the PR or the git history shows.

Write the history of a decision (the options compared, why the others were
not taken, and the checks of assumptions and research results) in the issue
instead of leaving it in an agent session. In the PR description, write only
the explanation of the diff (what changed and how, why, and how the diff was
checked). When unsure which one a text belongs to, ask whether it would change
if the diff changed: if so, write it in the PR; if not, in the issue. Do not
write records in PR comments, and do not put design documents in the
repository. These are records of their time, so do not update them when they
become outdated.

- Give each issue one label from the `Repo` group, named `<owner>/<repo>`, and
  one type label. The type is `Feature` when it adds a new feature or
  mechanism, `Improvement` when it improves something that exists (including
  documents, rules and CI), and `Bug` when it fixes something that does not
  work as intended. Because the label shows the repository, do not put a
  repository name in the title.
- When you create an issue, assign it to yourself. When an agent creates it,
  assign it to the person who asked (the user authenticated to Linear).
- When you create an issue, assess and set its priority, and never leave it at
  No priority. When unsure, pick the lower one, and revisit it when the
  situation changes.
  - `Urgent`: an outage of a running service, or a security problem, that
    must be fixed now.
  - `High`: needed for the goal currently in progress, or blocking other
    issues.
  - `Medium`: decided to do, and starting soon.
  - `Low`: an improvement or cleanup that is not urgent and can wait.

  Give a blocking issue the same or a higher priority than the issues it
  blocks.
- Set the status to one of the following, matching the stage of the work.
  - `Backlog`: something must still be decided before starting (the scope,
    the acceptance criteria, or the approach).
  - `Todo`: nothing is left to decide, and the work can start from the
    description alone.
  - `In Progress`: being worked on.
  - `In Review`: the work is finished and waits for a human to check it.
  - `Done`: all acceptance criteria are met.
  - `Canceled`: decided not to do it.
  - `Duplicate`: handed over to another issue.

  After you create or update an issue's description, check its status against
  these definitions, and move it to `Todo` when nothing is left to decide.
- Put blockers, remaining work and anything ruled out of scope in separate
  issues, and relate them to the original issue (blocked by, for a blocker).
- Write the current agreement (Goal, Scope, acceptance criteria and
  conclusion) in the description, so the current state is clear from the
  description alone. Write the conclusion as the adopted option in one line
  and link to the comment that approved it. Do not write the compared options
  or the reasoning in the description.
- Use comments for the conversation that builds the agreement (questions,
  objections and approvals of options, and reasons for changing the
  approach), and reflect the agreement in the description once reached. Do
  not write progress, work logs, verification records or research results in
  comments. When closing or pausing an issue without a PR, leave a short
  comment with the reason, or where the work stopped (the branch).
- Write research results (facts gathered as material for a decision, with
  their sources or how to check them) in a Linear document attached to the
  issue, and add further research to the same document. Do not create a
  document when there are no research results. Do not comment on the
  document; hold the discussion in the issue's comments.
- When the Goal changes, do not rewrite the description; create a new issue
  and relate it to the original one.
- Write acceptance criteria that are checked before merging. Check a workflow
  that runs only on the default branch by running it on the working branch,
  for example with `workflow_dispatch` or `act`. Do not make watching an
  effect, such as reduced CI time, an acceptance criterion; create a separate
  issue to measure it. Leave a criterion for after merging only when it needs
  the change to be on the default branch, and write such criteria apart from
  the others, each with one line on why it cannot be checked before merging.
- When a PR description refers to an issue, choose the magic word by what
  happens to the issue when the PR is merged, for each issue it refers to.
  - Merging meets all acceptance criteria: `Fixes <issue-id>`.
  - The PR is part of the issue's work but merging does not complete it:
    `Part of <issue-id>`. For an issue with post-merge checks left, write the
    results in its description, then move it to Done by hand.
  - The PR is not part of the issue's work but is related:
    `Related to <issue-id>`. Do not use this for the issue in the branch name.

Record the Claude Code session on the issue once, as soon as you pick or
create the issue and before creating a branch or changing files, using the
session ID from `CLAUDE_CODE_SESSION_ID`, so the work can be resumed with
`claude --resume <session-id>`. When the session has a URL, add it to the
issue's Resources as a link titled `Claude Code: <summary> (<session-id>)`;
otherwise post one comment with `Claude Code: <summary>` and
`claude --resume <session-id>`. Skip this outside Claude Code.

When an issue comment asks you to act, such as approving an option, reply
under that comment once the work is done, saying what you did and linking the
PR. Linear's tools cannot add reactions, so the reply shows the request was
handled. Write it once at the end, not while the work is in progress.

## Branches

Before starting work, run `git fetch` and create a new branch from the latest
default branch on `origin`. When you resume work on an existing branch, bring
in the changes from the default branch first. If the incoming changes touch
the rule files, read them again before continuing.

Name a branch you push `<author>/<issue-id>-<summary>`, with the issue ID in
lowercase and the agent's name as `<author>` (for example
`claude/abc-123-branch-naming`). Write the summary in English lowercase
letters, digits and hyphens; when Linear's "Copy git branch name" gives a
summary in another language, rewrite it. The default branch and branches
created by bots are exempt. If the environment assigns a branch name that does
not follow this rule, create one that does for the issue you are working on,
creating the issue first if none exists, and push there instead.

When you name a specific branch in an issue, PR or comment, link it as
`[<branch>](https://github.com/<owner>/<repo>/tree/<branch>)`.

## Pull Requests

Open a draft PR right after the first push, so the Linear issue links to it
and shows how far the work has gone. Keep a short list of done and remaining
work at the top of the PR description, mirroring the issue's pre-merge
acceptance criteria, and update it at every handoff. Leave out steps every PR
goes through, such as review. Mark the PR ready for review once every
criterion is verified.

When you handle review findings, reply in each finding's thread after pushing
the work, once per thread:

- For a finding you fixed, say in one line what you changed, link the commit,
  and resolve the thread.
- For a finding you disagree with, write both positions. For one you leave out
  of scope, give the reason and link the issue you created for it. Leave these
  threads unresolved for the human to decide.

## Reporting

At handoff, report the branch, remaining uncommitted changes, and whether the
commits were pushed. End the report with a link to each related Linear issue.
