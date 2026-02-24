# AI Caption Enhancement API Documentation

## Overview

The AI Caption Enhancement feature allows tenants to control whether their provided captions should be enhanced with AI or used as-is when scheduling posts.

## Caption Modes

The system supports three caption modes:

1. **Mode 1: AI-Generated** - Generate caption from scratch when no caption is provided
2. **Mode 2: Tenant-Customized** - Use tenant's caption exactly as provided (no AI enhancement)
3. **Mode 3: AI-Enhanced** - Enhance tenant's existing caption with AI based on their brand preferences

## API Endpoint

### Create Scheduled Post

**Endpoint:** `POST /api/tenant/posts`

**Request Body:**

```json
{
  "media_ids": [1, 2, 3],
  "caption": "Your caption text here",
  "platforms": ["instagram", "facebook"],
  "scheduled_at": "2025-01-15T10:00:00Z",
  "name": "Post Name",
  "enhance_caption": false
}
```

**Field: `enhance_caption`**

- **Type:** `boolean`
- **Default:** `false`
- **Required:** No

**Behavior:**

- `enhance_caption: false` (default) - Uses caption as-is (Mode 2: Tenant-Customized)
  - The scheduler will use the exact caption provided without any AI enhancement
  - Best for: Tenants who want full control over their caption text

- `enhance_caption: true` - Enhances caption with AI (Mode 3: AI-Enhanced)
  - The scheduler will enhance the provided caption using AI based on:
    - Tenant's brand persona and tone
    - Platform-specific best practices
    - Image analysis (if media URLs are available)
    - Business context (menu items, FAQs, etc.)
  - Best for: Tenants who want AI optimization while keeping their core message

**Special Case: Empty Caption**

- If `caption` is empty or not provided, the system will generate a caption from scratch (Mode 1: AI-Generated)
- The `enhance_caption` field is ignored in this case

## Response

**Success Response (201 Created):**

```json
{
  "success": true,
  "post_id": 123,
  "message": "Post scheduled successfully"
}
```

**Error Response (400 Bad Request):**

```json
{
  "error": "invalid_payload",
  "message": "media_ids required"
}
```

## Examples

### Example 1: Use Caption As-Is (Mode 2)

```bash
curl -X POST https://api.brancr.com/api/tenant/posts \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "media_ids": [1],
    "caption": "Check out our new product! #newproduct",
    "platforms": ["instagram"],
    "scheduled_at": "2025-01-15T10:00:00Z",
    "enhance_caption": false
  }'
```

**Result:** The caption "Check out our new product! #newproduct" will be used exactly as provided.

### Example 2: Enhance Caption with AI (Mode 3)

```bash
curl -X POST https://api.brancr.com/api/tenant/posts \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "media_ids": [1],
    "caption": "New product launch",
    "platforms": ["instagram"],
    "scheduled_at": "2025-01-15T10:00:00Z",
    "enhance_caption": true
  }'
```

**Result:** The caption "New product launch" will be enhanced with AI, potentially adding:
- Brand-appropriate tone and style
- Platform-optimized hashtags
- Improved engagement elements
- Image-based context

### Example 3: Generate Caption from Scratch (Mode 1)

```bash
curl -X POST https://api.brancr.com/api/tenant/posts \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "media_ids": [1],
    "caption": "",
    "platforms": ["instagram"],
    "scheduled_at": "2025-01-15T10:00:00Z"
  }'
```

**Result:** A caption will be generated from scratch based on:
- Tenant's brand persona
- Image analysis
- Business context
- Platform best practices

## Frontend Implementation Guide

### UI/UX Recommendations

1. **Caption Input Field**
   - Add a toggle or checkbox: "✨ Enhance with AI"
   - Default: Unchecked (use caption as-is)
   - When checked: Show tooltip explaining AI enhancement

2. **Visual Indicators**
   - Show different icons/badges for each mode:
     - Mode 1: 🤖 "AI Generated"
     - Mode 2: ✍️ "Your Caption"
     - Mode 3: ✨ "AI Enhanced"

3. **Preview**
   - When `enhance_caption: true`, show a preview of what the enhanced caption might look like
   - Allow users to toggle between original and enhanced preview

### React Example

```jsx
import { useState } from 'react';

function PostScheduler() {
  const [caption, setCaption] = useState('');
  const [enhanceCaption, setEnhanceCaption] = useState(false);

  const handleSubmit = async () => {
    const response = await fetch('/api/tenant/posts', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        media_ids: [1, 2, 3],
        caption: caption,
        platforms: ['instagram'],
        scheduled_at: '2025-01-15T10:00:00Z',
        enhance_caption: enhanceCaption, // Use this field
      }),
    });
  };

  return (
    <div>
      <textarea
        value={caption}
        onChange={(e) => setCaption(e.target.value)}
        placeholder="Write your caption..."
      />
      
      <label>
        <input
          type="checkbox"
          checked={enhanceCaption}
          onChange={(e) => setEnhanceCaption(e.target.checked)}
        />
        ✨ Enhance with AI
      </label>
      
      {enhanceCaption && (
        <p className="help-text">
          Your caption will be optimized for each platform using AI
        </p>
      )}
      
      <button onClick={handleSubmit}>Schedule Post</button>
    </div>
  );
}
```

### Vue Example

```vue
<template>
  <div>
    <textarea
      v-model="caption"
      placeholder="Write your caption..."
    />
    
    <label>
      <input
        type="checkbox"
        v-model="enhanceCaption"
      />
      ✨ Enhance with AI
    </label>
    
    <p v-if="enhanceCaption" class="help-text">
      Your caption will be optimized for each platform using AI
    </p>
    
    <button @click="submitPost">Schedule Post</button>
  </div>
</template>

<script>
export default {
  data() {
    return {
      caption: '',
      enhanceCaption: false,
    };
  },
  methods: {
    async submitPost() {
      const response = await fetch('/api/tenant/posts', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${this.token}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          media_ids: [1, 2, 3],
          caption: this.caption,
          platforms: ['instagram'],
          scheduled_at: '2025-01-15T10:00:00Z',
          enhance_caption: this.enhanceCaption, // Use this field
        }),
      });
    },
  },
};
</script>
```

## Migration Notes

- **Backward Compatibility:** Existing posts without `enhance_caption` field will default to `false` (use caption as-is)
- **Database:** The `enhance_caption` field is automatically added to the `scheduled_posts` table via GORM AutoMigrate
- **Default Behavior:** If not specified, `enhance_caption` defaults to `false` to maintain existing behavior

## Related Endpoints

- `GET /api/tenant/posts` - List scheduled posts (includes `enhance_caption` in response)
- `PUT /api/tenant/posts/:id` - Update scheduled post (supports updating `enhance_caption`)
- `GET /api/tenant/posts/:id` - Get post details (includes `enhance_caption` in response)

## Notes

- AI enhancement requires the tenant to have a configured brand persona and business profile
- Enhancement may take a few seconds during post execution
- If AI enhancement fails, the system falls back to using the original caption
- The enhanced caption is logged in the post activity logs for audit purposes

