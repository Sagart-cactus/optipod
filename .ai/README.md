# AI Assistant Configuration

This directory contains instructions and context for AI assistants working on the OptiPod project.

## Files

- **`instructions.md`** - Primary instructions for all AI assistants
  - Git workflow (branching, commits, CI checks)
  - Documentation update requirements
  - Code quality standards
  - Development commands

- **`context.md`** - Detailed project context
  - What OptiPod is and why it exists
  - Architecture and components
  - Core principles and constraints
  - Development workflow and best practices

## Usage

### For IDE Extensions (Cursor, Windsurf, Cline)
These tools typically read `.cursorrules` in the project root automatically.

### For CLI Tools (Claude Code, Codex, etc.)
Pass the instructions file to your CLI tool:

```bash
# Example with hypothetical CLI
your-ai-cli --instructions .ai/instructions.md

# Or pipe the content
cat .ai/instructions.md | your-ai-cli --system-prompt -

# Or combine both files
cat .ai/instructions.md .ai/context.md | your-ai-cli --system-prompt -
```

### For Kiro
Kiro automatically reads steering files from `.kiro/steering/` directory.

### For Web Interfaces (Claude.ai, ChatGPT)
Copy and paste the content of `instructions.md` and/or `context.md` into your conversation.

## File Relationships

```
Project Root
├── .cursorrules              # For Cursor, Windsurf, Cline (auto-read)
├── .ai/
│   ├── instructions.md       # Primary instructions (CLI tools)
│   ├── context.md           # Project context (CLI tools)
│   └── README.md            # This file
└── .kiro/
    └── steering/            # Kiro-specific steering files (auto-read by Kiro)
        ├── ai-documentation.md
        ├── git-workflow.md
        ├── documentation-updates.md
        └── project-context.md
```

## Key Rules Summary

1. **Always create new branches** - Never push to main
2. **Run CI checks first** - `./scripts/ci-checks-local.sh` before every commit
3. **Update both docs** - `docs/` (Markdown) and `website/` (HTML)
4. **AI docs in `.ai-docs/`** - Keep AI-generated docs separate
5. **Conventional commits** - Use `type: description` format

## Questions?

See the main project documentation:
- `README.md` - Project overview
- `CONTRIBUTING.md` - Contribution guidelines
- `docs/` - Technical documentation
