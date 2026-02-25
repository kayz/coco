# Browser Automation Debugging Guide

## Overview

When `lingti-bot` runs browser automation, it can now capture detailed debugging information to help diagnose issues. This guide explains how to enable and use the debug features.

## Quick Start

Enable debug mode when starting the router:

```bash
# Using command-line flags
lingti-bot router --provider deepseek --api-key sk-xxx \
  --debug \
  --debug-dir /tmp/lingti-debug

# Using environment variables
export BROWSER_DEBUG=1
export BROWSER_DEBUG_DIR=/tmp/lingti-debug
lingti-bot router --provider deepseek --api-key sk-xxx
```

## Debug Features

### 1. **Error Screenshots**

When browser actions fail (click, type, etc.), the system automatically captures screenshots of the current page state.

**Location:** Screenshots are saved to the debug directory with descriptive names:
- `error_click_resolve_ref42_2026-02-08_14-30-45.123.png` - Failed to resolve ref 42
- `error_click_not_interactable_ref15_2026-02-08_14-30-46.456.png` - Element not clickable
- `error_type_focus_ref8_2026-02-08_14-30-47.789.png` - Failed to focus input

### 2. **Fresh Snapshots in Errors**

When an action fails, the AI automatically receives a fresh snapshot of the current page state, showing:
- What elements are currently visible
- Updated ref numbers
- Current page structure

This helps the AI understand what changed and adjust its strategy.

### 3. **Automatic Retry**

When a ref is not found (page changed), the system automatically:
1. Captures a fresh snapshot
2. Updates the ref mappings
3. Retries the action with the updated refs

**Success message example:**
```
Clicked [15] button "Submit" (after auto-refresh)
```

## Configuration

### Command-Line Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--debug` | Enable browser debug mode | `false` |
| `--debug-dir <path>` | Directory for debug screenshots | `/tmp/lingti-bot` (Unix)<br>`%TEMP%\lingti-bot` (Windows) |

### Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `BROWSER_DEBUG` | Enable debug mode | `1` or `true` |
| `BROWSER_DEBUG_DIR` | Debug directory path | `/tmp/lingti-debug` |

### Platform Defaults

- **macOS/Linux:** `/tmp/lingti-bot/`
- **Windows:** `%TEMP%\lingti-bot\`

## Debug Workflow

### Normal Operation (No Debug)

```
1. User: "Click the login button"
2. AI: browser_snapshot
3. AI: browser_click ref=15
4. ❌ Error: "element not interactable"
5. User sees: "failed to click ref 15: element not interactable"
```

### With Debug Mode

```
1. User: "Click the login button"
2. AI: browser_snapshot
3. AI: browser_click ref=15
4. ❌ Error detected
5. System: Captures screenshot → /tmp/lingti-bot/error_click_not_interactable_ref15_....png
6. System: Captures fresh snapshot
7. AI sees:
   - Original error message
   - Screenshot location
   - Current page snapshot with updated refs
8. AI: "The button is covered by a modal. Let me close the modal first."
9. AI: browser_click ref=22 (close modal)
10. AI: browser_click ref=15 (retry login button)
```

## Debugging Common Issues

### Issue: "ref not found"

**Cause:** Page changed after snapshot (navigation, AJAX, animations)

**Automatic Fix:** System retries with fresh snapshot

**Manual Fix:** Run `browser_snapshot` again before retrying the action

### Issue: "element not interactable"

**Cause:** Element is hidden, covered, or not yet loaded

**Debug:** Check the error screenshot to see:
- Is the element visible?
- Is there a modal/overlay covering it?
- Did the page finish loading?

**Fix:**
- Wait for page to load
- Close overlays
- Scroll element into view
- Use a different selector

### Issue: "click failed"

**Cause:** Element cannot be clicked (disabled, removed, etc.)

**Debug:** Error screenshot shows the exact page state when click failed

**Fix:**
- Check if element is disabled
- Wait for element to become enabled
- Find alternative way to achieve the goal

## Best Practices

1. **Always check screenshots first** - They show exactly what the automation saw
2. **Look for patterns** - Multiple failures at the same step? Check timing/waits
3. **Compare before/after** - Screenshots help identify what changed
4. **Clean up debug directory** - Old screenshots can accumulate quickly

```bash
# Clean up debug directory
rm -rf /tmp/lingti-bot/*.png

# Or rotate by date
find /tmp/lingti-bot -name "*.png" -mtime +7 -delete
```

## Example: Debugging a Login Flow

### User Request
> "Go to example.com and log in with my credentials"

### Debug Session

```bash
# Start with debug enabled
lingti-bot router --debug --debug-dir /tmp/login-debug \
  --provider deepseek --api-key sk-xxx
```

**AI Actions:**
1. `browser_navigate` → example.com
2. `browser_snapshot` → Gets refs for all elements
3. `browser_click ref=12` → Click username field
4. ❌ **Error:** "element not interactable"
   - Screenshot saved: `/tmp/login-debug/error_click_not_interactable_ref12_...png`
   - Shows: Cookie consent modal is covering the form!
5. AI analyzes fresh snapshot, sees modal
6. `browser_click ref=45` → Accept cookies
7. `browser_click ref=12` → ✅ Username field focused
8. `browser_type ref=12 text="user"` → ✅ Typed username
9. `browser_type ref=13 text="pass" submit=true` → ✅ Logged in

### Debug Files Created
```
/tmp/login-debug/
├── error_click_not_interactable_ref12_2026-02-08_14-30-45.123.png
```

**Reviewing the screenshot shows:**
- Cookie modal was covering the login form
- AI correctly identified and closed the modal
- Login succeeded after modal was removed

## Performance Impact

Debug mode has minimal performance impact:
- Screenshots are only captured on **errors**
- No screenshots during successful operations
- Disk I/O is asynchronous

**Typical overhead:** < 100ms per error

## Disabling Debug Mode

Simply restart without the `--debug` flag:

```bash
lingti-bot router --provider deepseek --api-key sk-xxx
```

Or unset the environment variable:

```bash
unset BROWSER_DEBUG
unset BROWSER_DEBUG_DIR
```

## Troubleshooting

### Debug directory not created

**Check permissions:**
```bash
ls -ld /tmp/lingti-bot
mkdir -p /tmp/lingti-bot
chmod 755 /tmp/lingti-bot
```

### No screenshots being saved

**Verify debug mode is enabled:**
- Check command-line flags
- Check environment variables
- Look for "Browser debug mode enabled" in startup logs

### Screenshots are blank

**Possible causes:**
- Page hasn't loaded yet
- Browser is in headless mode (expected)
- Page rendering issue

## Summary

Debug mode provides three key benefits:

1. **Visual feedback** - Screenshots show exactly what went wrong
2. **Automatic recovery** - Retry with fresh snapshots when refs are stale
3. **Context for AI** - AI sees current page state and can adapt strategy

Enable it during development and testing, disable in production for optimal performance.
