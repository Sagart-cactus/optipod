---
inclusion: always
---

# Git Workflow Guidelines

## Branch Strategy

**ALWAYS create a new branch before pushing code changes.**

Never push directly to `main` or the current branch. When the user asks to "push the code" or "commit and push", follow this workflow:

### Required Steps

1. **Create a descriptive branch name** based on the type of change:
   - `feature/` - New features
   - `fix/` - Bug fixes
   - `chore/` - Maintenance tasks, documentation updates
   - `refactor/` - Code refactoring
   - `test/` - Test additions or modifications

   Examples:
   - `feature/add-prometheus-auth`
   - `fix/helm-deployment-issue`
   - `chore/update-dependencies`
   - `refactor/simplify-metrics-provider`

2. **Run CI checks locally BEFORE committing**:
   ```bash
   ./scripts/ci-checks-local.sh
   ```
   
   - If checks fail, fix the issues before proceeding
   - Do not commit code that fails CI checks
   - Report any failures to the user and ask for guidance if needed

3. **Create the branch**:
   ```bash
   git checkout -b <branch-name>
   ```

4. **Stage and commit changes**:
   ```bash
   git add <files>
   git commit -m "descriptive commit message"
   ```

5. **Push to the new branch**:
   ```bash
   git push -u origin <branch-name>
   ```

6. **Create a Pull Request** (if requested or appropriate):
   ```bash
   gh pr create --title "..." --body "..."
   ```

## Commit Message Format

Follow conventional commit format:

```
<type>: <short description>

<optional longer description>

<optional footer>
```

Types:
- `feat:` - New feature
- `fix:` - Bug fix
- `chore:` - Maintenance tasks
- `docs:` - Documentation changes
- `refactor:` - Code refactoring
- `test:` - Test changes
- `ci:` - CI/CD changes

## Example Workflow

When user says "push the code":

```bash
# 1. Run CI checks first
./scripts/ci-checks-local.sh

# 2. If checks pass, create branch
git checkout -b feature/new-feature

# 3. Stage changes
git add .

# 4. Commit with descriptive message
git commit -m "feat: add new feature for X

- Implemented Y
- Added tests for Z
- Updated documentation"

# 5. Push to new branch
git push -u origin feature/new-feature

# 6. Create PR (if appropriate)
gh pr create --title "feat: add new feature for X" --body "..."
```

## Important Notes

- **Never skip CI checks** - They catch issues before they reach CI/CD
- **Always use descriptive branch names** - Makes it easy to understand what the branch contains
- **Write clear commit messages** - Helps with code review and git history
- **Create PRs for review** - Don't merge directly unless explicitly instructed

## Exceptions

The only time to push to the current branch is when:
1. User explicitly says "push to current branch" or "amend and force push"
2. Already working on a feature branch and adding more commits
3. Fixing issues in an existing PR branch
