package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             string
	DatabaseURL      string
	MigrationsDir    string
	BatchConcurrency int
	TCGPlayerCookie  string
	CardSalesLimit   int
	TesseractBin     string
	VisionOCRLangs   string
	VisionMaxImages  int
}

func Load() (Config, error) {
	cfg := Config{
		Port:             envOrDefault("PORT", "18080"),
		DatabaseURL:      envOrDefault("DATABASE_URL", "file:market-api.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"),
		MigrationsDir:    envOrDefault("MIGRATIONS_DIR", "./migrations"),
		BatchConcurrency: 4,
		TCGPlayerCookie:  strings.TrimSpace(os.Getenv("TCGPLAYER_COOKIE")),
		CardSalesLimit:   10,
		TesseractBin:     envOrDefault("TESSERACT_BIN", "tesseract"),
		VisionOCRLangs:   envOrDefault("VISION_OCR_LANGS", "eng+jpn"),
		VisionMaxImages:  4,
	}

	if raw := os.Getenv("BATCH_CONCURRENCY"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return Config{}, fmt.Errorf("invalid BATCH_CONCURRENCY: %q", raw)
		}
		cfg.BatchConcurrency = value
	}

	if raw := os.Getenv("CARD_SALES_LIMIT"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return Config{}, fmt.Errorf("invalid CARD_SALES_LIMIT: %q", raw)
		}
		cfg.CardSalesLimit = value
	}

	if raw := os.Getenv("VISION_MAX_IMAGES"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return Config{}, fmt.Errorf("invalid VISION_MAX_IMAGES: %q", raw)
		}
		cfg.VisionMaxImages = value
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
