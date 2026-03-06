# market-api

Standalone Go API for normalizing inbound listing data, classifying items, and resolving market metadata.

## Current Status

This initial scaffold includes:

- config loading
- SQLite database bootstrap and migrations
- `GET /healthz`
- `POST /v1/resolve`
- `POST /v1/resolve/batch`
- `GET /v1/requests/{id}`
- `GET /v1/results/{id}`
- basic One Piece text classification for singles and sealed products
- request fingerprint caching
- direct `card` package TCGplayer resolution
- local Tesseract OCR for One Piece image analysis
- optional `image_urls` multi-photo input
- English vs Japanese printing detection with TCGplayer skip for strong JP signals
- raw provider payload persistence in `provider_results`

## Run

```bash
go run ./cmd/server
```

Environment variables:

- `PORT` default `18080`
- `DATABASE_URL` default `file:market-api.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)`
- `MIGRATIONS_DIR` default `./migrations`
- `BATCH_CONCURRENCY` default `4`
- `TCGPLAYER_COOKIE` optional
- `CARD_SALES_LIMIT` default `10`
- `TESSERACT_BIN` default `tesseract`
- `VISION_OCR_LANGS` default `eng+jpn`
- `VISION_MAX_IMAGES` default `4`
- `MARKET_API_AUTH_TOKEN` optional shared secret for all endpoints except `GET /healthz`

`market-api` imports `github.com/Buckelew/card` directly as a private Go module.

## Mercari Monitor Scripts

Two cron-friendly scripts now live in this repo and use `market-api` for valuation:

- `./scripts/run_mercari_saved_monitor.sh`
  - scans `cari saved-queries scan`
  - hydrates each new or changed listing with `cari get-listing`
  - calls `POST /v1/resolve/batch`
  - posts every resolved finding to `GENERAL_WEBHOOK_URL`
  - posts threshold-passing deals to `DEALS_WEBHOOK_URL`

- `./scripts/run_mercari_deals_monitor.sh`
  - searches Mercari `--deals` terms through `cari search`
  - hydrates each new or changed listing with `cari get-listing`
  - calls `POST /v1/resolve/batch`
  - posts every resolved finding to `GENERAL_WEBHOOK_URL`
  - posts threshold-passing deals to `DEALS_WEBHOOK_URL`

Setup:

```bash
cp scripts/mercari_saved_monitor.env.example scripts/mercari_saved_monitor.env
cp scripts/mercari_deals_monitor.env.example scripts/mercari_deals_monitor.env
chmod +x scripts/run_mercari_saved_monitor.sh scripts/run_mercari_deals_monitor.sh
```

Requirements:

- `cari` authenticated for saved-query scans
- `market-api` running locally or `MARKET_API_BASE_URL` pointed at a reachable instance
- Discord webhook URLs
- if `market-api` has `MARKET_API_AUTH_TOKEN` set, the monitor env must also set `MARKET_API_AUTH_TOKEN`

Examples:

```bash
./scripts/run_mercari_saved_monitor.sh
./scripts/run_mercari_deals_monitor.sh
```

Notes:

- First run defaults to `BOOTSTRAP_ON_EMPTY_STATE=true`, which records current listings without alerting.
- State is stored separately for each monitor under `state/`.
- Deals use the same ROI/confidence math as the earlier Mercari deal monitor: buyer fee, shipping, `MIN_ROI`, and `MIN_CONFIDENCE`.

## Generate Mercari Image Fixtures

To build a local One Piece image fixture set from Mercari with card codes removed from text fields:

```bash
make generate-image-testdata
```

This writes [one_piece_image_cases.json](/Users/caden/Documents/Coding/Personal/market-api/testdata/one_piece_image_cases.json) and includes:

- original Mercari title and description
- redacted title and description with the detected card code removed
- `request_redacted` payloads
- `request_image_only` payloads
- image URLs and expected card code / language labels
