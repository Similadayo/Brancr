# Social Package Structure

This package handles all social media platform integrations.

## Current Structure

- `connector.go` - Defines the `SocialConnector` interface and global `Connectors` registry
- `scheduler.go` - Post scheduling and execution
- `message_handler.go` - Handles incoming messages from platforms
- `meta_webhook.go` - Meta (Instagram/Facebook/WhatsApp) webhook handler
- `webhooks.go` - General webhook handling
- `uploader.go` - Media upload utilities
- `verifier.go` - Token verification
- `manual_auth.go` - Manual authentication flows

## Platform Connectors

Platform-specific implementations:

- `instagram.go` - Instagram Graph API integration
- `facebook.go` - Facebook Graph API integration
- `whatsapp.go` - WhatsApp Business API integration
- `tiktok.go` - TikTok Business API integration
- `youtube.go` - YouTube API integration

Each platform connector:

- Implements the `SocialConnector` interface
- Auto-registers itself via `init()` function
- Handles platform-specific posting logic

## Organization Note

Platform connector files (`instagram.go`, `facebook.go`, etc.) are currently in the root `social` package. They use `init()` functions to auto-register with the global `Connectors` map defined in `connector.go`.

This structure keeps all platform code easily accessible and avoids import complexity. Future reorganization could move platform files to a `platforms/` subpackage if needed, but this would require updating imports throughout the codebase.

