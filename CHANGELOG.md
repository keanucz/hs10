# Changelog

## [Unreleased]

### Added
- @mention autocomplete for agents
  - Type `@` to see list of available agents
  - Navigate with arrow keys
  - Select with Enter or Tab
  - Click to select
  - Supports: `@pm`, `@backend`, `@frontend`
- Improved chat scrolling
  - Smooth scroll behavior
  - Custom styled scrollbar (thin, rounded)
  - Better visibility and hover effects
  - Works in Chrome, Safari, and Firefox

### Changed
- Agent names now display in chat (e.g., "Product Manager" instead of "Agent")
- Agent avatars show initials (PM, BA, FE)
- Chat area now has visible, styled scrollbar

### Fixed
- Fixed agent name display in message sender
- Improved agent identification in chat interface
- Fixed chat layout: messages now scroll correctly within viewport
- Fixed message input box staying at bottom (no longer pushed off screen)
- Fixed flexbox layout constraints for proper height management

## Features

### @Mention System

**Triggers:**
- `@pm` - Product Manager
- `@backend` - Backend Architect
- `@frontend` - Frontend Developer

**How to use:**
1. Type `@` in the message input
2. Autocomplete dropdown appears
3. Use arrow keys or mouse to select
4. Press Enter/Tab or click to insert
5. Finish typing your message
6. Agent will respond to your mention

**Fallback:** Keywords still work if you don't use @mentions.

### Agent Display

**Before:**
- All agents showed as "Agent"
- Avatar was generic "A"

**After:**
- Shows full name: "Product Manager", "Backend Architect", "Frontend Developer"
- Avatars show initials: "PM", "BA", "FE"
- Easier to distinguish between agents

## Usage Examples

### Using @mentions
```
@pm help me plan this feature
@backend what database should we use?
@frontend create a login component
```

### Using keywords (still works)
```
I need to build a feature  → Product Manager
Design the API              → Backend Architect
Create a UI component       → Frontend Developer
```

## Technical Details

**Frontend:**
- Autocomplete dropdown with keyboard navigation
- Real-time @mention detection
- CSS-styled dropdown with hover/selection states

**Backend:**
- @mention detection prioritized over keywords
- Case-insensitive matching
- Falls back to keyword detection if no @mention found

**Keyboard Shortcuts:**
- `Arrow Down` - Next agent
- `Arrow Up` - Previous agent
- `Enter` or `Tab` - Select agent
- `Escape` - Close autocomplete
- Click outside - Close autocomplete
