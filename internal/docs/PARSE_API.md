# Quick Add / Parse API

This document describes the product/menu parsing endpoints used by the Quick Add frontend feature.

All endpoints require tenant authentication (Tenant JWT via middleware.TenantAuth).

POST /tenant/products/parse
- Purpose: Parse pasted text synchronously
- Body: { raw_input: string, source?: "paste" }
- Responses:
  - 200: JSON array of ParsedItem
  - 400: invalid body or use file/image endpoints
  - 413: input too large
  - 429: rate limit / quota exceeded

POST /tenant/products/parse/file
- Purpose: Upload CSV/DOCX/TXT and parse
- Request: multipart/form-data { file: <file> }
- Behavior: Small files processed synchronously; large files (>2MB) accepted and processed asynchronously (202)
- Responses:
  - 200: JSON array of ParsedItem
  - 202: accepted (processing in background)
  - 400/413/429: as above

POST /tenant/products/parse/image
- Purpose: Upload an image for OCR + parse
- Request: multipart/form-data { image: <image> }
- Response: { ocr_text: string, items: [ParsedItem] }

Async processing
- POST /tenant/products/parse/async or upload large files to `/parse/file` will return 202 and enqueue a background job. A job status endpoint will be available in later phases.

ParsedItem schema
- name: string
- price: number|null
- currency: ISO code string ("NGN"|"USD"|"GBP"|"EUR" etc.)
- type: string (optional)
- category: string (optional)
- flags: object (e.g., { price_missing: true })
- confidence: number (0.0 - 1.0)

Notes
- Max paste size: 64 KB
- Max parsed items: 1000 (if exceeded, return 413 and ask user to upload file)
- Rate limiting: endpoints are protected by tenant API quota middleware; clients should handle 429 responses and `Retry-After` as appropriate.

Operational considerations
- Default parser is rule-based; an AI-backed parser is used for larger inputs when available.
- AI output is validated and will fall back to rule-based parser on parse errors.
- For heavy/long-running workloads (large files, OCR), use async enqueue to avoid blocking requests.
