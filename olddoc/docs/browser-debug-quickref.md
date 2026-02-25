# Browser Debug Quick Reference

## Enable Debug Mode

```bash
# Method 1: Command-line flags
lingti-bot router --debug --debug-dir /tmp/lingti-debug --provider deepseek --api-key sk-xxx

# Method 2: Environment variables
export BROWSER_DEBUG=1
export BROWSER_DEBUG_DIR=/tmp/lingti-debug
lingti-bot router --provider deepseek --api-key sk-xxx
```

## What Gets Captured

| Event | Screenshot | Fresh Snapshot | Auto-Retry |
|-------|------------|----------------|------------|
| Click fails | ✅ | ✅ | ✅ (if ref not found) |
| Type fails | ✅ | ✅ | ✅ (if ref not found) |
| Element not found | ✅ | ✅ | ✅ |
| Element not interactable | ✅ | ✅ | ❌ |
| Successful action | ❌ | ❌ | N/A |

## Screenshot Naming

```
error_{action}_{reason}_ref{number}_{timestamp}.png
```

**Examples:**
- `error_click_resolve_ref42_2026-02-08_14-30-45.123.png`
- `error_click_not_interactable_ref15_2026-02-08_14-30-46.456.png`
- `error_type_focus_ref8_2026-02-08_14-30-47.789.png`
- `error_type_input_ref10_2026-02-08_14-30-48.789.png`

## Default Directories

| Platform | Default Debug Dir |
|----------|-------------------|
| macOS / Linux | `/tmp/lingti-bot/` |
| Windows | `%TEMP%\lingti-bot\` |

## Common Error Messages

### With Debug Disabled
```
failed to click ref 15: element not interactable
```

### With Debug Enabled
```
failed to click ref 15: element not interactable
(debug screenshot saved to: /tmp/lingti-bot/error_click_not_interactable_ref15_2026-02-08_14-30-45.123.png)

Current page snapshot (refs may have changed):
[1] button "Accept Cookies"
[2] link "Login"
[3] textbox "Username"
...
```

## Automatic Retry Flow

```
1. Action fails with "ref not found"
   ↓
2. Capture fresh snapshot
   ↓
3. Update ref mappings
   ↓
4. Retry action automatically
   ↓
5. Success: "Clicked [15] button "Submit" (after auto-refresh)"
   OR
   Failure: Return error with fresh snapshot
```

## Maintenance

```bash
# View recent debug screenshots
ls -lht /tmp/lingti-bot/*.png | head -10

# Clean up old screenshots (older than 7 days)
find /tmp/lingti-bot -name "*.png" -mtime +7 -delete

# Clean all debug screenshots
rm -rf /tmp/lingti-bot/*.png
```

## Performance

- **No impact** during successful operations
- **~50-100ms** overhead per error (screenshot capture)
- Screenshots only created on **failures**
- Recommended for development/testing, optional for production
