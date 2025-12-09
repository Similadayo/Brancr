# Telegram Bot: AI Caption Enhancement Implementation

## Overview

The Telegram bot now properly handles AI caption enhancement preferences when tenants create posts. This document describes the implementation details and user flow.

## User Flow

### Flow 1: Write Your Own Caption (Mode 2 or Mode 3)

1. **User selects "✍️ Write my own"**
   - Bot sets `CaptionMode = "write"`
   - User types their caption

2. **After user submits caption, bot shows options:**
   ```
   ✅ Your Caption:
   [User's caption text]
   
   [✅ Use As-Is] [✨ AI Enhance]
   [✏️ Edit]
   ```

3. **User clicks "✅ Use As-Is"**
   - Sets `UseAICaption = false`
   - Sets `EnhanceCaption = false` in ScheduledPost
   - Scheduler uses caption as-is (Mode 2: Tenant-Customized)
   - Continues to platform selection

4. **User clicks "✨ AI Enhance"**
   - Sets `UseAICaption = true`
   - Calls `enhanceCaptionInSession()` to enhance caption immediately
   - Shows enhanced caption with options:
     ```
     ✨ AI-Enhanced Caption:
     [Enhanced caption text]
     
     [✅ Use This] [🔄 Rephrase]
     [✨ Fine-tune] [✏️ Edit]
     ```
   - When user clicks "✅ Use This", `UseAICaption` remains `true`
   - Sets `EnhanceCaption = true` in ScheduledPost
   - Scheduler will enhance the caption again during execution (Mode 3: AI-Enhanced)

### Flow 2: Keywords → AI (Mode 1)

1. **User selects "🔑 Keywords → AI"**
   - Bot sets `CaptionMode = "keywords"`
   - User provides keywords/phrases
   - Bot generates caption from keywords
   - Shows generated caption with options to use, rephrase, or edit

### Flow 3: AI Generate (Mode 1)

1. **User selects "🤖 AI generate"**
   - Bot sets `CaptionMode = "ai"`
   - Sets `UseAICaption = true`
   - Caption is left empty
   - Sets `EnhanceCaption = true` in ScheduledPost (though caption is empty)
   - Scheduler generates caption from scratch (Mode 1: AI-Generated)

## Implementation Details

### Session State Management

The `PostSession` struct tracks:
- `CaptionMode`: "write", "keywords", "ai", or ""
- `UseAICaption`: boolean indicating if AI should be used
- `Caption`: the current caption text

### Key Functions

#### 1. Caption Mode Selection (`post_flow.go`)

When user selects caption mode:
- **"write"**: Sets `CaptionMode = "write"`, prompts for caption
- **"keywords"**: Sets `CaptionMode = "keywords"`, prompts for keywords
- **"ai"**: Sets `CaptionMode = "ai"` and `UseAICaption = true`, skips to platform selection

#### 2. Caption Submission (`post_flow.go`, Step 3)

When user submits caption in "write" mode:
```go
if sess.CaptionMode == "write" {
    sess.Caption = text
    // Show options: "Use As-Is", "AI Enhance", "Edit"
}
```

#### 3. Use As-Is Handler (`tenant_handler.go`)

```go
if arg == "use" {
    if sess.CaptionMode == "write" && !sess.UseAICaption {
        sess.UseAICaption = false  // Explicitly set to false
    }
    sess.Step = 4  // Continue to platform selection
}
```

#### 4. AI Enhance Handler (`tenant_handler.go`)

```go
case "enhance":
    sess.UseAICaption = true  // Set flag for scheduler
    h.enhanceCaptionInSession(bot, tenantID, chatID, sess)
```

#### 5. Post Creation (`post_flow.go`)

When creating ScheduledPost:
```go
sp := storage.ScheduledPost{
    // ... other fields ...
    EnhanceCaption: sess.UseAICaption,  // Store preference
}
```

## Button Callbacks

### Caption Options (After Writing)

| Button | Callback | Action |
|--------|----------|--------|
| ✅ Use As-Is | `caption:use` | Sets `UseAICaption = false`, continues to platforms |
| ✨ AI Enhance | `caption:enhance` | Sets `UseAICaption = true`, enhances caption |
| ✏️ Edit | `caption:edit` | Returns to caption input |

### After AI Enhancement

| Button | Callback | Action |
|--------|----------|--------|
| ✅ Use This | `caption:use` | Keeps `UseAICaption = true`, continues to platforms |
| 🔄 Rephrase | `caption:rephrase` | Rephrases the enhanced caption |
| ✨ Fine-tune | `caption:fine_tune` | Fine-tunes the enhanced caption |
| ✏️ Edit | `caption:edit` | Returns to caption input |

## State Transitions

```
Start Post Flow
    ↓
Select Caption Mode
    ↓
┌─────────────────────────────────────┐
│ Write Mode                           │
│  ↓                                   │
│ User types caption                   │
│  ↓                                   │
│ [Use As-Is] → UseAICaption = false   │
│ [AI Enhance] → UseAICaption = true   │
│  ↓                                   │
│ Platform Selection                   │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│ Keywords Mode                        │
│  ↓                                   │
│ User provides keywords               │
│  ↓                                   │
│ Bot generates caption                │
│  ↓                                   │
│ [Use This] → UseAICaption = true    │
│  ↓                                   │
│ Platform Selection                   │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│ AI Generate Mode                     │
│  ↓                                   │
│ UseAICaption = true                  │
│ Caption = ""                         │
│  ↓                                   │
│ Platform Selection                   │
│  ↓                                   │
│ Scheduler generates caption         │
└─────────────────────────────────────┘
```

## Error Handling

### AI Enhancement Fails

If `enhanceCaptionInSession()` fails:
- Bot shows: "Sorry, I couldn't enhance the caption right now. Using your original caption."
- Sets `UseAICaption = false` (implicitly)
- Continues with original caption (Mode 2 fallback)
- Proceeds to platform selection

### Scheduler Fallback

If scheduler enhancement fails:
- Logs failure in post activity logs
- Uses original caption as fallback
- Post still publishes successfully

## User Messages

### Caption Mode Selection
```
How would you like to handle the caption?

[✍️ Write my own] [🔑 Keywords → AI]
[🤖 AI generate] [⏭️ Skip]
```

### After Writing Caption
```
✅ Your Caption:

[User's caption text]

[✅ Use As-Is] [✨ AI Enhance]
[✏️ Edit]
```

### After AI Enhancement
```
✨ AI-Enhanced Caption:

[Enhanced caption text]

[✅ Use This] [🔄 Rephrase]
[✨ Fine-tune] [✏️ Edit]
```

## Testing Scenarios

### Test 1: Use Caption As-Is
1. Start post flow
2. Select "Write my own"
3. Type caption: "Check out our new product!"
4. Click "Use As-Is"
5. **Expected:** `EnhanceCaption = false` in database
6. **Expected:** Scheduler uses caption exactly as provided

### Test 2: Enhance Caption
1. Start post flow
2. Select "Write my own"
3. Type caption: "New product"
4. Click "AI Enhance"
5. **Expected:** Enhanced caption shown
6. Click "Use This"
7. **Expected:** `EnhanceCaption = true` in database
8. **Expected:** Scheduler enhances caption during execution

### Test 3: AI Generate
1. Start post flow
2. Select "AI generate"
3. **Expected:** `UseAICaption = true`, `Caption = ""`
4. **Expected:** Scheduler generates caption from scratch

## Code Locations

- **Caption Mode Selection:** `internal/bot/post_flow.go` (lines 154-191)
- **Caption Submission:** `internal/bot/post_flow.go` (lines 169-191)
- **Callback Handlers:** `internal/bot/tenant_handler.go` (lines 861-926)
- **AI Enhancement:** `internal/bot/tenant_handler.go` (lines 2078-2144)
- **Post Creation:** `internal/bot/post_flow.go` (lines 703-713)

## Notes

- The `UseAICaption` flag in the session determines the `EnhanceCaption` value in ScheduledPost
- If user enhances caption in bot, it's enhanced immediately for preview, then enhanced again by scheduler during execution
- This double enhancement allows users to see what the enhanced caption looks like before scheduling
- The scheduler enhancement uses the most up-to-date tenant preferences and image analysis

