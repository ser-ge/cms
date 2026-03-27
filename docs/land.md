# cms land

Land the current feature branch into a target branch with rebase, merge, and cleanup.

## Usage

```bash
cms land [target] [flags]
```

Target resolution: `[worktree].base_branch` from project `.cms.toml` → `[worktree].base_branch` from user `config.toml` → `git config branch.<name>.cms-base` (recorded at worktree creation) → `origin/HEAD` → local `main` → local `master`. Supports symbols: `^` (default branch), `-` (previous), `@` (current).

## Flags

| Flag | Description |
|------|-------------|
| `--no-squash` | Preserve individual commits (skip squash, backup ref, and staging) |
| `-m "msg"` | Commit message for the squash commit |
| `--no-edit` | Don't open editor for squash commit message |
| `--no-ff` | Create a merge commit even when fast-forward is possible |
| `--keep` | Don't remove worktree/branch/tmux window after landing |
| `--abort` | Abort an in-progress rebase |
| `--continue` | Resume after resolving rebase conflicts |
| `--autostash` | Stash dirty target worktree without prompting |

## Pipeline

The land workflow executes these steps in order:

```
1. Stage uncommitted changes                          (skipped with --no-squash)
2. Run pre_commit hooks                               (skipped with --no-squash)
3. Save backup ref to refs/cms-wt-backup/<branch>     (skipped with --no-squash)
4. Squash commits into one                            (skipped with --no-squash)
5. Run post_commit hooks                              (skipped with --no-squash)
6. Rebase onto target
7. Run pre_merge hooks               (pre-land)
8. Fast-forward merge into target    (--no-ff: merge commit)
9. Run post_merge hooks              (post-land)
10. Remove worktree + branch + tmux  (--keep: skip)
```

### Step details

**Squash (steps 3-4):** Saves the current HEAD to `refs/cms-wt-backup/<branch>` so original commit history can be recovered (`git log refs/cms-wt-backup/<branch>`). Then `git reset --soft <merge-base>` + single commit.

**Squash commit message** (in priority order):

1. **`-m "message"`** — explicit message from the command line
2. **`[worktree].commit_cmd`** — the staged diff is piped via stdin to the configured command. The prompt includes a diff summary (`git diff --stat`) and the full diff (truncated at 8KB). On failure, falls through to the next option with a warning. Example commands:
   - `claude -p --no-session-persistence --model=haiku --tools='' --disable-slash-commands --setting-sources='' --system-prompt=''`
   - `llm -m claude-haiku-4.5`
   - `CLAUDECODE= claude -p --model=haiku ...` (prefix `CLAUDECODE=` when running inside a Claude Code session)
3. **Interactive editor** — if no `-m` and no `commit_cmd` (or it failed), opens `$EDITOR` for manual entry. Skipped with `--no-edit`.
4. **Default** — `"Merge branch '<name>'"` + `git diff --stat` output

**Rebase (step 5):** Standard `git rebase <target>`. On conflict, exits with instructions to resolve and run `cms land --continue`.

**Merge (step 7):** Default is `--ff-only`. If fast-forward fails, automatically falls back to a merge commit. With `--no-ff`, always creates a merge commit. The merge runs in the target branch's worktree if one exists, otherwise checks out the target in the main worktree.

**Cleanup (step 9):** Runs `pre_remove` hooks, removes the worktree (`git worktree remove`), deletes the branch (`git branch -d`), and kills the tmux window. Before cleanup, switches the tmux client to the target worktree's window and waits for Enter confirmation.

## Conflict recovery

When the rebase hits conflicts:

```bash
# Fix conflicts in the working tree, then:
git add <resolved-files>
git rebase --continue

# Then finish the land:
cms land --continue

# Or abort everything:
cms land --abort
```

`--continue` defers branch resolution until after the rebase finishes. During a conflicted rebase HEAD is detached, so the merge step must wait until HEAD is back on the branch.

## Hooks

Land invokes up to five hook stages from `[worktree]` config:

| Stage | Config key | When | Failure behavior |
|-------|-----------|------|-----------------|
| Pre-commit | `pre_commit` | Before squash commit | Aborts land |
| Post-commit | `post_commit` | After squash commit | Warning only |
| Pre-land | `pre_merge` | Before merge into target | Aborts land |
| Post-land | `post_merge` | After merge into target | Warning only |
| Pre-remove | `pre_remove` | Before worktree removal | Warning only |

All hooks receive `CMS_WORKTREE_PATH` and `CMS_REPO_ROOT` environment variables.

## Configuration

```toml
[worktree]
base_branch = "main"                  # default target for cms land
commit_cmd = "claude -p --no-session-persistence --model=haiku --tools='' --disable-slash-commands --setting-sources='' --system-prompt=''"

[[worktree.pre_commit]]
command = "npm run lint"

[[worktree.pre_merge]]
command = "npm test"

[[worktree.post_merge]]
command = "echo 'landed!'"
```

## Examples

```bash
# Default: squash + rebase + ff-merge into default branch, then cleanup
cms land

# Land into a specific branch
cms land develop

# Squash with explicit commit message
cms land -m "Add authentication system"

# Preserve individual commits (no squash, no backup ref)
cms land --no-squash

# Land but keep the worktree around
cms land --keep

# Force merge commit (no fast-forward)
cms land --no-ff
```

## Worktree-aware merge

When the target branch has its own worktree (e.g. `main` is checked out in `~/projects/myapp`), the merge runs directly in that worktree. No `git checkout` is needed — this avoids disrupting work in the main worktree.

When the target has no worktree, land checks out the target in the main worktree, merges, then proceeds with cleanup.

If the target worktree has uncommitted changes, land prompts to stash them before merging and pops them back after. Use `--autostash` to skip the prompt. If the stash pop conflicts, your changes stay in the stash — run `git stash list` in the target worktree to find them.

## Backup refs

When using `--squash`, land saves the pre-squash HEAD to `refs/cms-wt-backup/<branch>` before collapsing commits. This lets you recover the original history:

```bash
git log refs/cms-wt-backup/my-feature    # view original commits
git cherry-pick <sha>                     # recover a specific commit
```

Backup refs persist until manually deleted (`git update-ref -d refs/cms-wt-backup/<branch>`).
