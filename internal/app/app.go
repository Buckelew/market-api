package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/Buckelew/card/tcgplayer"

	"market-api/internal/api"
	"market-api/internal/classify"
	"market-api/internal/config"
	"market-api/internal/db"
	cardprovider "market-api/internal/providers/card"
	visionprovider "market-api/internal/providers/vision"
	"market-api/internal/resolve"
)

type App struct {
	db      *sql.DB
	handler http.Handler
}

func New(cfg config.Config) (*App, error) {
	database, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if err := db.ApplyMigrations(context.Background(), database, cfg.MigrationsDir); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	store := db.NewStore(database)
	classifier := classify.New()
	vision := visionprovider.New(cfg.TesseractBin, cfg.VisionOCRLangs, cfg.VisionMaxImages, cfg.OllamaURL, cfg.OllamaModel, cfg.VisionAPIKey, cfg.VisionAPIBase, cfg.VisionModel)
	cardClient := tcgplayer.NewClient(cfg.TCGPlayerCookie)
	card := cardprovider.New(cardClient, cfg.CardSalesLimit)
	resolver := resolve.NewService(store, classifier, vision, card)
	server := api.NewServer(store, resolver, cfg.BatchConcurrency, cfg.AuthToken)

	return &App{
		db:      database,
		handler: server.Routes(),
	}, nil
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) Close() error {
	return a.db.Close()
}
