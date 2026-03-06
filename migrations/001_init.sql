CREATE TABLE IF NOT EXISTS resolve_requests (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    source_listing_id TEXT NOT NULL,
    source_url TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    image_url TEXT NOT NULL,
    normalized_title TEXT NOT NULL,
    normalized_description TEXT NOT NULL,
    normalized_image_url TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    request_json TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS classification_results (
    request_id TEXT PRIMARY KEY,
    product_type TEXT NOT NULL,
    category TEXT NOT NULL,
    market TEXT NOT NULL,
    confidence REAL NOT NULL,
    reasons_json TEXT NOT NULL,
    signals_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (request_id) REFERENCES resolve_requests(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS resolve_results (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL UNIQUE,
    response_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (request_id) REFERENCES resolve_requests(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS provider_results (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    market TEXT NOT NULL,
    query TEXT NOT NULL,
    raw_payload TEXT NOT NULL,
    normalized_json TEXT NOT NULL,
    confidence REAL NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (request_id) REFERENCES resolve_requests(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cache_entries (
    key TEXT PRIMARY KEY,
    cache_type TEXT NOT NULL,
    value_json TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
