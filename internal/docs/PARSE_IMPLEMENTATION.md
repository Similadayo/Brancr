Parse feature: Implementation notes and production setup

Overview
- Quick Add parsing supports: paste text, CSV/TXT/DOCX uploads, and image OCR.
- Default parser: rule-based heuristics (implemented in `DefaultParser`).
- AI parser: `AIParser` wraps `ai.AIClientInterface` and is used for larger or complex inputs.
- Background processing: large file uploads are enqueued as `parse:import` jobs and processed by the job queue.

Production setup
1) Job Queue
- Ensure `Redis` is configured and `JobQueue` is initialized via `core.NewServices`.
- The parse import job uses `queue.RegisterParseImportHandler` and saves parsed results as a draft with key `import.products` in the `drafts` table.

2) OCR Service
- Implement the `tenantapi.OCRService` interface with a provider of your choice (Tesseract, Google Vision, etc.).
- Set the implementation on the tenant API handler in `core/router.go` after `NewHandler` is called:
  apiHandler.OCRService = &yourOCRImplementation{}

3) AI integration
- Configure `OPENAI_API_KEY` and `AI_MODEL` in production env to enable AI parsing.
- AIParser will be used for inputs larger than 200 characters by default; adjust threshold in `parse_handler.go` if needed.

Security & limits
- Paste input max length: 64KB
- Max parsed items per request: 1000 (use file upload for larger imports)
- API quota and per-tenant rate limiting are enforced by `middleware.APIQuota` and rate limiter middleware.

Testing & observability
- Unit tests cover rule-based parsing, CSV parsing and docx extraction helpers, OCR handler (with fake OCR), and AI fallback behavior.
- Instrumentation: basic metrics counters were added to `Metrics` for HTTP/AI requests and quota rejections (extend as needed).

Notes & future improvements
- For large imports we now upload the file to blob storage (S3/MinIO) and enqueue a parse job that contains the `bucket` and `key` reference (no base64 in payload). The job handler fetches the file from blobstore and parses it.
- Job status is recorded in Redis under key `parse:job:{job_id}` and a job status endpoint has been added: `GET /tenant/products/parse/jobs/{job_id}`. The endpoint returns the stored job status JSON: pending|done|failed and result metadata (e.g., draft_id on success).
- Consider adding X-RateLimit headers in APIQuota middleware to help UIs show remaining quota.
