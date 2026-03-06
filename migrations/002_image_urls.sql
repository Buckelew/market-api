ALTER TABLE resolve_requests ADD COLUMN image_urls_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE resolve_requests ADD COLUMN normalized_image_urls_json TEXT NOT NULL DEFAULT '[]';
