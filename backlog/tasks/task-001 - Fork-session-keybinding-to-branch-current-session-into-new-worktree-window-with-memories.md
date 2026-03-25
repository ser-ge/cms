---
id: TASK-001
title: >-
  Fork session: keybinding to branch current session into new worktree/window
  with memories
status: To Do
assignee: []
created_date: '2026-03-25 23:37'
labels:
  - feature
dependencies: []
priority: medium
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add a cms command (and tmux keybinding) that forks the current session: creates a new git worktree branched from the current branch, opens it in a new tmux window/session, and copies/symlinks Claude Code project memories so the new session has full context from the original.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 cms fork [branch-name] creates worktree, tmux window, and memory link
- [ ] #2 Claude Code memories from original path are accessible in new worktree
- [ ] #3 CLAUDE.md and in-repo context files carry over via worktree
<!-- AC:END -->
