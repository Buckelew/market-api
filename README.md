# market-api

Takes a raw marketplace listing and tells you what it is and what it sells for.

I buy and sell One Piece cards, and Mercari listings are messy: titles are often vague or wrong, the card code is often only in the photo, and Japanese prints get mixed in with English ones. This API does the lookup I was doing by hand. You send it a title, description, and photos. It sends back the card, the language, and recent TCGplayer prices.

## How a listing gets resolved

1. The input is cleaned up and hashed (SHA-256). If the same listing was already resolved, the saved answer comes back right away.
2. Regex rules pull out the card code (like `OP05-119`), set, language, sealed product type (booster box, pack, starter deck), quantity, and whether it's a lot.
3. Each photo goes through OCR twice: once whole, and once cropped to the bottom third, where One Piece cards print their code. Hits from the crop count for more. OCR uses a local Ollama model and falls back to Tesseract. If `VISION_API_KEY` is set, a vision model reads the photos first and OCR is the backup.
4. Text and photo results are combined. If they disagree, the response includes a warning.
5. Singles and sealed product are matched on TCGplayer, with recent sales. If the photos show a Japanese print, the TCGplayer lookup is skipped, since the English price would be wrong.

Every provider's raw response is saved in SQLite, so you can see why a match came out the way it did.

Batch requests run with a fixed number of workers (`BATCH_CONCURRENCY`, default 4).

## API

```
GET  /healthz
POST /v1/resolve          one listing
POST /v1/resolve/batch    many listings
GET  /v1/requests/{id}
GET  /v1/results/{id}
```

## Building it

This won't build as-is outside my machine. It depends on two modules of mine that aren't public, `github.com/Buckelew/card` (the TCGplayer client) and a local `discogs` module pulled in with a `replace` in `go.mod`.

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

## Mercari monitor scripts

Two cron-friendly scripts use `market-api` to find deals on Mercari and post them to Discord:

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
- A listing counts as a deal when its ROI after buyer fee and shipping clears `MIN_ROI` and the match confidence clears `MIN_CONFIDENCE`.

## Generate Mercari image fixtures

To build a local One Piece image fixture set from Mercari with card codes removed from text fields:

```bash
make generate-image-testdata
```

This writes [`testdata/one_piece_image_cases.json`](testdata/one_piece_image_cases.json) and includes:

- original Mercari title and description
- redacted title and description with the detected card code removed
- `request_redacted` payloads
- `request_image_only` payloads
- image URLs and expected card code / language labels
