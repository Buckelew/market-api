package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"market-api/internal/mercariwatch"
)

const (
	defaultDealsStateFile = "state/mercari_deals_monitor.json"
	defaultMaxSeen        = 10000
)

type config struct {
	CariBin               string
	StateFile             string
	SearchTerms           []string
	SearchLimit           int
	HydrateLimit          int
	MaxSeen               int
	BootstrapOnEmptyState bool
	DryRun                bool

	MarketAPIBaseURL  string
	CommandTimeout    time.Duration
	ResolveTimeout    time.Duration
	WebhookTimeout    time.Duration
	WebhookPause      time.Duration
	DiscordUsername   string
	GeneralWebhookURL string
	DealsWebhookURL   string

	Category string

	DealParams mercariwatch.DealParams
}

type candidate struct {
	Item  mercariwatch.SearchItem
	Label string
}

func main() {
	log.SetFlags(log.LstdFlags)

	if err := mercariwatch.LoadDotEnv(mercariwatch.EnvString("ENV_FILE", filepath.Join("scripts", "mercari_deals_monitor.env"))); err != nil {
		log.Fatalf("load env: %v", err)
	}

	cfg := loadConfig()
	if len(cfg.SearchTerms) == 0 {
		log.Fatalf("SEARCH_TERMS must not be empty")
	}
	if strings.TrimSpace(cfg.GeneralWebhookURL) == "" && strings.TrimSpace(cfg.DealsWebhookURL) == "" && !cfg.DryRun {
		log.Fatalf("GENERAL_WEBHOOK_URL or DEALS_WEBHOOK_URL is required unless DRY_RUN=true")
	}

	ctx := context.Background()
	cari := &mercariwatch.CariClient{
		Bin:            cfg.CariBin,
		CommandTimeout: cfg.CommandTimeout,
	}
	apiClient := &mercariwatch.MarketAPIClient{
		BaseURL:   cfg.MarketAPIBaseURL,
		Client:    &http.Client{Timeout: cfg.ResolveTimeout},
		AuthToken: mercariwatch.EnvString("MARKET_API_AUTH_TOKEN", ""),
	}
	webhookClient := &http.Client{Timeout: cfg.WebhookTimeout}

	candidates := make(map[string]candidate)
	for _, term := range cfg.SearchTerms {
		items, err := cari.SearchDeals(ctx, term, cfg.SearchLimit)
		if err != nil {
			log.Printf("search failed for %q: %v", term, err)
			continue
		}
		for _, item := range items {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				continue
			}
			if _, exists := candidates[id]; exists {
				continue
			}
			candidates[id] = candidate{
				Item:  item,
				Label: "Deals Search: " + term,
			}
		}
	}

	if len(candidates) == 0 {
		log.Printf("no Mercari deal-search listings found")
		return
	}

	state, err := mercariwatch.LoadState(cfg.StateFile)
	if err != nil {
		log.Fatalf("load state: %v", err)
	}

	if len(state.Entries) == 0 && cfg.BootstrapOnEmptyState {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		for id, cand := range candidates {
			state.Entries[id] = mercariwatch.StateEntry{
				LastSeenAt:     now,
				LastPriceCents: cand.Item.Price,
			}
			state.Order = append(state.Order, id)
		}
		mercariwatch.TrimState(state, cfg.MaxSeen)
		if err := mercariwatch.SaveState(cfg.StateFile, state); err != nil {
			log.Fatalf("save bootstrap state: %v", err)
		}
		log.Printf("bootstrapped %d deal-search listing(s); no alerts sent", len(candidates))
		return
	}

	pending := pendingCandidates(candidates, state)
	if cfg.HydrateLimit > 0 && len(pending) > cfg.HydrateLimit {
		pending = pending[:cfg.HydrateLimit]
	}
	if len(pending) == 0 {
		log.Printf("no new or changed deal-search listings")
		return
	}

	listings := make([]*mercariwatch.Listing, 0, len(pending))
	requests := make([]mercariwatch.ResolveRequestInput, 0, len(pending))
	for _, cand := range pending {
		listing, err := cari.GetListing(ctx, cand.Item.ID)
		if err != nil {
			log.Printf("get-listing failed for %s, using search payload: %v", cand.Item.ID, err)
			listing = mercariwatch.ListingFromSearchItem(cand.Item)
		}
		listings = append(listings, listing)
		requests = append(requests, buildResolveRequest(listing, cfg.Category))
	}

	resultsByIndex, err := apiClient.ResolveBatch(ctx, requests)
	if err != nil {
		log.Fatalf("resolve batch: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	generalSent := 0
	dealSent := 0
	for i, cand := range pending {
		listing := listings[i]
		entry := state.Entries[cand.Item.ID]
		entry.LastSeenAt = now
		entry.LastPriceCents = listing.Price

		var resolved *mercariwatch.ResolveResponse
		var resolveErr string
		if i < len(resultsByIndex) {
			resolved = resultsByIndex[i].Result
			resolveErr = resultsByIndex[i].Error
		} else {
			resolveErr = "missing batch result"
		}

		priceKey := fmt.Sprintf("%d", listing.Price)
		if shouldSendGeneral(cfg, entry, priceKey, resolved) {
			payload := mercariwatch.BuildFindingPayload(cfg.DiscordUsername, cand.Label, listing, resolved, resolveErr)
			if cfg.DryRun {
				log.Printf("DRY_RUN general alert: %s", mercariwatch.MercariListingURL(cand.Item.ID))
			} else if err := mercariwatch.PostWebhook(ctx, webhookClient, cfg.GeneralWebhookURL, payload); err != nil {
				log.Printf("general webhook failed for %s: %v", cand.Item.ID, err)
			} else {
				entry.LastGeneralKey = priceKey
				generalSent++
				pause(cfg.WebhookPause)
			}
		}

		score := mercariwatch.ScoreResolvedDeal(listing.Price, listing.ShippingClass.Fee, resolved, cfg.DealParams)
		if shouldSendDeal(cfg, entry, priceKey, score) {
			payload := mercariwatch.BuildDealPayload(cfg.DiscordUsername, cand.Label, listing, resolved, score)
			if cfg.DryRun {
				log.Printf("DRY_RUN deal alert: %s", mercariwatch.MercariListingURL(cand.Item.ID))
			} else if err := mercariwatch.PostWebhook(ctx, webhookClient, cfg.DealsWebhookURL, payload); err != nil {
				log.Printf("deal webhook failed for %s: %v", cand.Item.ID, err)
			} else {
				entry.LastDealKey = priceKey
				dealSent++
				pause(cfg.WebhookPause)
			}
		}

		state.Entries[cand.Item.ID] = entry
		state.Order = append(state.Order, cand.Item.ID)
	}

	mercariwatch.TrimState(state, cfg.MaxSeen)
	if err := mercariwatch.SaveState(cfg.StateFile, state); err != nil {
		log.Fatalf("save state: %v", err)
	}

	log.Printf("processed %d deal-search listing(s), sent %d finding alert(s), %d deal alert(s)", len(pending), generalSent, dealSent)
}

func loadConfig() config {
	return config{
		CariBin:               mercariwatch.EnvString("CARI_BIN", "cari"),
		StateFile:             mercariwatch.EnvString("STATE_FILE", defaultDealsStateFile),
		SearchTerms:           mercariwatch.EnvCSV("SEARCH_TERMS", defaultSearchTerms()),
		SearchLimit:           mercariwatch.EnvInt("SEARCH_LIMIT", 40),
		HydrateLimit:          mercariwatch.EnvInt("HYDRATE_LIMIT", 25),
		MaxSeen:               mercariwatch.EnvInt("MAX_SEEN", defaultMaxSeen),
		BootstrapOnEmptyState: mercariwatch.EnvBool("BOOTSTRAP_ON_EMPTY_STATE", true),
		DryRun:                mercariwatch.EnvBool("DRY_RUN", false),
		MarketAPIBaseURL:      mercariwatch.EnvString("MARKET_API_BASE_URL", "http://127.0.0.1:18080"),
		CommandTimeout:        mercariwatch.Seconds(mercariwatch.EnvInt("COMMAND_TIMEOUT_SEC", 25)),
		ResolveTimeout:        mercariwatch.Seconds(mercariwatch.EnvInt("RESOLVE_TIMEOUT_SEC", 30)),
		WebhookTimeout:        mercariwatch.Seconds(mercariwatch.EnvInt("WEBHOOK_TIMEOUT_SEC", 10)),
		WebhookPause:          time.Duration(mercariwatch.EnvInt("WEBHOOK_PAUSE_MS", 250)) * time.Millisecond,
		DiscordUsername:       mercariwatch.EnvString("DISCORD_USERNAME", "Mercari Deals Monitor"),
		GeneralWebhookURL:     strings.TrimSpace(mercariwatch.EnvString("GENERAL_WEBHOOK_URL", "")),
		DealsWebhookURL:       strings.TrimSpace(mercariwatch.EnvString("DEALS_WEBHOOK_URL", "")),
		Category: mercariwatch.EnvString("CATEGORY", ""),
		DealParams: mercariwatch.DealParams{
			MinROI:             mercariwatch.EnvFloat("MIN_ROI", 0.30),
			MinConfidence:      mercariwatch.EnvFloat("MIN_CONFIDENCE", 0.80),
			BuyerFeePercent:    mercariwatch.EnvFloat("BUYER_FEE_PERCENT", 0.036),
			DefaultShippingUSD: mercariwatch.EnvFloat("DEFAULT_SHIPPING_USD", 1.50),
		},
	}
}

func pendingCandidates(candidates map[string]candidate, state *mercariwatch.StateStore) []candidate {
	out := make([]candidate, 0, len(candidates))
	for id, cand := range candidates {
		entry, ok := state.Entries[id]
		if !ok || entry.LastPriceCents != cand.Item.Price {
			out = append(out, cand)
		}
	}
	return out
}

func buildResolveRequest(listing *mercariwatch.Listing, category string) mercariwatch.ResolveRequestInput {
	if listing == nil {
		return mercariwatch.ResolveRequestInput{}
	}
	imageURLs := mercariwatch.ListingImageURLs(listing)
	imageURL := ""
	if len(imageURLs) > 0 {
		imageURL = imageURLs[0]
	}
	return mercariwatch.ResolveRequestInput{
		Source:          "mercari",
		SourceListingID: listing.ID,
		SourceURL:       mercariwatch.MercariListingURL(listing.ID),
		Title:           listing.Name,
		Description:     listing.Description,
		ImageURL:        imageURL,
		ImageURLs:       imageURLs,
		Category:        category,
	}
}

func shouldSendGeneral(cfg config, entry mercariwatch.StateEntry, priceKey string, resolved *mercariwatch.ResolveResponse) bool {
	if strings.TrimSpace(cfg.GeneralWebhookURL) == "" {
		return false
	}
	if mercariwatch.IsJapaneseListing(resolved) {
		return false
	}
	return entry.LastGeneralKey != priceKey
}

func shouldSendDeal(cfg config, entry mercariwatch.StateEntry, priceKey string, score mercariwatch.DealScore) bool {
	if strings.TrimSpace(cfg.DealsWebhookURL) == "" {
		return false
	}
	if !score.Pass {
		return false
	}
	return entry.LastDealKey != priceKey
}

func pause(d time.Duration) {
	if d > 0 {
		time.Sleep(d)
	}
}

func defaultSearchTerms() []string {
	out := make([]string, 0, 63)
	add := func(prefix string, n int) {
		code := fmt.Sprintf("%02d", n)
		out = append(out, prefix+" "+code, prefix+"-"+code, prefix+code)
	}
	for i := 1; i <= 14; i++ {
		add("op", i)
	}
	for i := 1; i <= 4; i++ {
		add("eb", i)
	}
	for i := 1; i <= 3; i++ {
		add("prb", i)
	}
	return out
}
