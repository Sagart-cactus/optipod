---
inclusion: always
---

# Documentation Update Guidelines

## Dual Documentation Requirement

When the user asks to "change the documentation", "update the docs", or make any documentation changes, **ALWAYS update BOTH locations:**

1. **`docs/` folder** - Markdown documentation for developers
2. **`website/` folder** - HTML/web documentation for users

## Documentation Structure

### docs/ Folder
- **Format**: Markdown (.md files)
- **Audience**: Developers, contributors, technical users
- **Purpose**: Technical documentation, API references, development guides
- **Examples**:
  - Installation guides
  - Configuration references
  - API documentation
  - Development workflows
  - Troubleshooting guides

### website/ Folder
- **Format**: HTML files (with embedded CSS/JS)
- **Audience**: End users, operators, general audience
- **Purpose**: User-facing documentation, getting started guides, tutorials
- **Examples**:
  - Getting started pages
  - Feature overviews
  - Configuration examples
  - Best practices
  - FAQ

## Update Workflow

When updating documentation:

1. **Identify the topic** - What documentation needs to change?

2. **Update docs/ folder**:
   - Create or modify the relevant .md file
   - Use clear markdown formatting
   - Include code examples
   - Add links to related docs

3. **Update website/ folder**:
   - Create or modify the corresponding HTML file
   - Maintain consistent styling with existing pages
   - Ensure responsive design
   - Add navigation links if needed
   - Test that HTML renders correctly

4. **Keep content synchronized**:
   - Both versions should cover the same information
   - Adapt the format to the medium (MD vs HTML)
   - Ensure examples and commands are identical
   - Update version numbers consistently

## Content Mapping

Common documentation topics and their locations:

| Topic | docs/ Location | website/ Location |
|-------|---------------|-------------------|
| Installation | `docs/installation.md` | `website/docs/installation/` |
| Configuration | `docs/configuration.md` | `website/docs/configuration/` |
| Prometheus Auth | `docs/prometheus-auth.md` | `website/docs/prometheus-authentication/` |
| Getting Started | `docs/getting-started.md` | `website/docs/getting-started/` |
| API Reference | `docs/api-reference.md` | `website/docs/api/` |
| Troubleshooting | `docs/troubleshooting.md` | `website/docs/troubleshooting/` |

## When to Update Documentation

Update documentation when:
- ✅ Adding new features
- ✅ Changing configuration options
- ✅ Modifying behavior or APIs
- ✅ Adding new examples
- ✅ Fixing errors or clarifying content
- ✅ User explicitly requests documentation changes

## Documentation Quality Standards

### Markdown (docs/)
- Use proper heading hierarchy (# ## ###)
- Include code blocks with language tags
- Add links to related documentation
- Keep line length reasonable
- Use tables for structured data

### HTML (website/)
- Follow existing page structure
- Use semantic HTML5 elements
- Maintain consistent navigation
- Ensure mobile responsiveness
- Include proper meta tags
- Test cross-browser compatibility

## Example Workflow

User says: "Update the Prometheus authentication documentation"

**Steps:**
1. Update `docs/prometheus-auth.md` with new content
2. Update `website/docs/prometheus-authentication/index.html` with same content
3. Ensure code examples match in both locations
4. Verify links work in both versions
5. Commit both changes together

## Important Notes

- **Never update only one location** - Always update both
- **Keep content in sync** - Same information, different formats
- **Test HTML rendering** - Ensure website pages display correctly
- **Update navigation** - If adding new pages, update menus/indexes
- **Version consistency** - Keep version numbers aligned across both

## Exceptions

Only update one location when:
- Creating internal development notes (docs/ only)
- Updating website-specific assets (website/ only, e.g., CSS, images)
- User explicitly specifies "only update docs/" or "only update website/"
