# cms land

Land the current feature branch into a target branch with rebase, merge, and cleanup.

## Usage

```bash
cms land [target] [flags]
```

Target defaults to `[worktree].base_branch`, then auto-detected default branch (`origin/HEAD` → `main` → `master`). Supports symbols: `^` (default branch), `-` (previous), `@` (current).

## Flags

| Flag | Description |
|------|-------------|
| `--squash` | Squash all commits into one before landing |
| `-m "msg"` | Commit message for squash (requires `--squash`) |
| `--no-edit` | Don't open editor for squash commit message |
| `--no-ff` | Create a merge commit even when fast-forward is possible |
| `--keep` | Don't remove worktree/branch/tmux window after landing |
| `--abort` | Abort an in-progress rebase |
| `--continue` | Resume after resolving rebase conflicts |

## Pipeline

The land workflow executes these steps in order:

```
1. Stage uncommitted changes         (--squash only)
2. Run pre_commit hooks
3. Squash commits                    (--squash only)
4. Run post_commit hooks
5. Rebase onto target
6. Run pre_merge hooks               (pre-land)
7. Fast-forward merge into target    (--no-ff: merge commit)
8. Run post_merge hooks              (post-land)
9. Remove worktree + branch + tmux   (--keep: skip)
```

### Step details

**Squash (step 3):** `git reset --soft <merge-base>` + single commit. If `[worktree].commit_cmd` is configured, the staged diff is piped to it for LLM-generated commit messages (diff truncated at 8KB). Falls back to `"Merge branch '<name>'"` + diffstat on failure. If no message is provided and `--no-edit` is not set, opens the editor.

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
commit_cmd = "llm -m claude-haiku"    # LLM commit message generation

[[worktree.pre_commit]]
command = "npm run lint"

[[worktree.pre_merge]]
command = "npm test"

[[worktree.post_merge]]
command = "echo 'landed!'"
```

## Examples

```bash
# Basic land: rebase + ff-merge into default branch, then cleanup
cms land

# Land into a specific branch
cms land develop

# Squash all feature commits, auto-generate message via LLM
cms land --squash

# Squash with explicit message
cms land --squash -m "Add authentication system"

# Land but keep the worktree around
cms land --keep

# Force merge commit (no fast-forward)
cms land --no-ff
```

## Worktree-aware merge

When the target branch has its own worktree (e.g. `main` is checked out in `~/projects/myapp`), the merge runs directly in that worktree. No `git checkout` is needed — this avoids disrupting work in the main worktree.

When the target has no worktree, land checks out the target in the main worktree, merges, then proceeds with cleanup.
