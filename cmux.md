# cmux Findings

Looked at `cmux` as a source of ideas for `cms`.

## Best Ideas to Reuse

### 1. Attention / unread model

cmux has a clear concept of unread or attention-needed workspaces and jump-to-attention behavior.
For `cms`, the equivalent would be:

- sessions with agent `waiting`
- sessions with newly completed work
- sessions with failures or notable events

This maps well to the current agent activity model.

### 2. Notification pipeline

cmux has a stronger notification model than `cms`.
We could reuse the idea for:

- waiting-for-input notifications
- task completion notifications
- long-running task completion

### 3. Compact workspace summaries

cmux shows useful metadata per workspace without forcing everything into the main content area.
For `cms`, that suggests:

- better finder summaries
- optional compact session badges
- richer attention summaries instead of more dashboard columns

### 4. Jump to latest attention

This was the clearest actionable idea.
A `cms` command that jumps to the most recent or highest-priority attention-needed session would fit naturally.

## What Not to Copy

- Browser integration
- Full sidebar/app-shell concepts
- Heavier workspace metadata systems that do not fit tmux directly

## Recommended Next Steps

1. Add an explicit attention queue model on top of current activity detection.
2. Add a command to jump to the newest or highest-priority attention item.
3. Consider optional local notifications for waiting and completion.
