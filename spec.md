# market-api Spec

## Overview

Create a new standalone Go app in a sibling directory, `market-api`, with its own separate database. The API accepts raw listing fields, classifies the item, resolves market data, stores full raw provider payloads internally, and returns normalized results with confidence scores. It does not fetch Mercari listings itself.

## Core Decisions

- Separate app
- Go
- Separate DB
- Inputs: `title`, `description`, `image_url` / `image_urls` plus optional source metadata
- Mercari fetch stays outside this app
- Return all results regardless of confidence
- No `cache_hit` field in response
- Batch endpoint uses bounded concurrency
- Store full raw provider payloads internally
- Public responses stay normalized

## Public API

- `GET /healthz`
- `POST /v1/resolve`
- `POST /v1/resolve/batch`
- `GET /v1/requests/{id}`
- `GET /v1/results/{id}`

## Request Contract

```json
{
  "source": "optional",
  "source_listing_id": "optional",
  "source_url": "optional",
  "title": "optional",
  "description": "optional",
  "image_url": "optional",
  "image_urls": ["optional"]
}
```

## Normalized Response Contract

```json
{
  "request_id": "uuid",
  "classification": {
    "product_type": "trading_card|unknown",
    "category": "one_piece_tcg|unknown",
    "market": "tcgplayer|unknown",
    "confidence": 0.97,
    "reasons": ["..."]
  },
  "signals": {
    "card_code": "OP14-033",
    "image_language": "en",
    "image_confidence": 0.93
  },
  "market_match": {
    "matched": true,
    "provider": "card",
    "market": "tcgplayer",
    "query": "OP14-033 Perona",
    "card_number": "OP14-033",
    "product_id": "668342",
    "product_name": "Perona",
    "market_price": 21.52,
    "recent_median_sale": 20.75,
    "confidence": 0.98
  },
  "warnings": []
}
```

## Architecture

```text
market-api/
  cmd/server/
  internal/api/
  internal/app/
  internal/config/
  internal/db/
  internal/models/
  internal/classify/
  internal/resolve/
  internal/providers/
    card/
    vision/
  internal/cache/
  migrations/
  README.md
  go.mod
```

## Responsibility Boundaries

- `api/`: handlers, validation, JSON responses
- `classify/`: product/category detection
- `resolve/`: market selection and orchestration
- `providers/card/`: `card` CLI integration
- `providers/vision/`: image URL analysis
- `cache/`: request and provider cache
- `db/`: persistence
- `app/`: composition and lifecycle

## V1 Resolver Pipeline

1. Accept request
2. Normalize fields and compute fingerprint
3. Check request cache
4. Classify from title/description
5. If needed and `image_url` exists, run image analysis
6. If image analysis strongly suggests a Japanese printing, skip TCGplayer lookup
7. Select resolver
8. Query provider
9. Normalize provider result
10. Compute confidence
11. Persist request, result, and full raw payloads
12. Return normalized response

## V1 Supported Classification/Resolution

- Product type: `trading_card`
- Category: `one_piece_tcg`
- Market: `tcgplayer`
- Provider: `card`
- Fallback category: `unknown`

## Batch Processing

Implement `POST /v1/resolve/batch` with bounded concurrency:

- worker pool
- configurable concurrency cap
- preserve output order
- independent per-item processing
- structured per-item result entries

## Persistence

Store:

- original request JSON
- normalized request fields
- request fingerprint
- classification output
- extracted signals
- normalized provider result
- full raw provider payloads
- timestamps and status fields

Suggested tables:

- `resolve_requests`
- `resolve_results`
- `classification_results`
- `provider_results`
- `cache_entries`

## Caching

Internal only, no cache metadata in API response.

Cache layers:

- request cache by normalized fingerprint
- provider cache by normalized lookup query

Suggested TTLs:

- provider cache: 15 to 60 minutes
- request cache: up to 24 hours for identical input

## Mercari Boundary

Mercari is an external caller:

- another app uses `cari`
- that app extracts fields
- that app calls `market-api`

## Milestones

1. Scaffold new Go app in sibling directory.
2. Add config, DB, migrations, health endpoint.
3. Implement `POST /v1/resolve`.
4. Add persistence and normalized response model.
5. Add One Piece text classification.
6. Add image URL fallback analysis.
7. Add `card` resolver for TCGplayer.
8. Add request/provider caching.
9. Add `POST /v1/resolve/batch` with bounded concurrency.
10. Add request/result retrieval endpoints.
11. Add tests and rate limiting.

## Testing

- normalization tests
- classifier tests
- confidence tests
- cache tests
- provider parsing tests
- API handler tests
- batch concurrency/order tests
- persistence tests

## Instructions For A New Agent

- Create a new sibling project, not changes inside the existing repo
- Build a standalone Go API
- Keep the API source-agnostic
- Do not fetch Mercari data inside this service
- Return normalized outputs only
- Store full raw provider payloads internally
- Return all results, including low-confidence ones
