package resolve

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"market-api/internal/classify"
	"market-api/internal/db"
	"market-api/internal/models"
)

type fakeCardProvider struct {
	cardQueries   []string
	sealedQueries []string
	cardResult    *models.ProviderLookupResult
	sealedResult  *models.ProviderLookupResult
	cardErr       error
	sealedErr     error
	blockCard     bool
	blockSealed   bool
}

type fakeVisionProvider struct {
	result *models.VisionAnalysisResult
	err    error
}

func (f *fakeVisionProvider) AnalyzeOnePiece(_ context.Context, _, _ string, _ []string) (*models.VisionAnalysisResult, error) {
	return f.result, f.err
}

func (f *fakeCardProvider) LookupOnePieceCard(ctx context.Context, query string) (*models.ProviderLookupResult, error) {
	f.cardQueries = append(f.cardQueries, query)
	if f.blockCard {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.cardResult, f.cardErr
}

func (f *fakeCardProvider) LookupOnePieceSealed(ctx context.Context, query string) (*models.ProviderLookupResult, error) {
	f.sealedQueries = append(f.sealedQueries, query)
	if f.blockSealed {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.sealedResult, f.sealedErr
}

func TestResolveUsesCardLookupForTradingCards(t *testing.T) {
	store := newTestStore(t)
	provider := &fakeCardProvider{
		cardResult: &models.ProviderLookupResult{
			MarketMatch:    models.MarketMatch{Matched: true, Provider: "card", Market: "tcgplayer", ProductID: "1", ProductName: "Perona"},
			ProviderResult: models.ProviderResult{Provider: "card", Market: "tcgplayer", Query: "OP14-033"},
		},
	}
	service := NewService(store, classify.New(), nil, provider)

	_, err := service.Resolve(context.Background(), models.ResolveRequestInput{Title: "OP14-033 Perona"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(provider.cardQueries) != 1 {
		t.Fatalf("expected card lookup once, got %d", len(provider.cardQueries))
	}
	if len(provider.sealedQueries) != 0 {
		t.Fatalf("expected no sealed lookup")
	}
}

func TestResolveUsesSealedLookupForSealedProducts(t *testing.T) {
	store := newTestStore(t)
	provider := &fakeCardProvider{
		sealedResult: &models.ProviderLookupResult{
			MarketMatch:    models.MarketMatch{Matched: true, Provider: "card", Market: "tcgplayer", ProductID: "2", ProductName: "Booster Box"},
			ProviderResult: models.ProviderResult{Provider: "card", Market: "tcgplayer", Query: "OP10 booster box"},
		},
	}
	service := NewService(store, classify.New(), nil, provider)

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{Title: "One Piece OP10 Booster Box"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if response.Classification.ProductType != "sealed_product" {
		t.Fatalf("expected sealed_product, got %q", response.Classification.ProductType)
	}
	if len(provider.sealedQueries) != 1 {
		t.Fatalf("expected sealed lookup once, got %d", len(provider.sealedQueries))
	}
	if len(provider.cardQueries) != 0 {
		t.Fatalf("expected no card lookup")
	}
}

func TestMergeSignalsUsesOCRWhenTextMissing(t *testing.T) {
	merged := mergeSignals(
		models.Signals{SealedType: "booster_box"},
		models.Signals{SetCode: "OP10", SetName: "Royal Blood", Quantity: 24},
	)

	if merged.SetCode != "OP10" {
		t.Fatalf("mergeSignals() set code = %q, want OP10", merged.SetCode)
	}
	if merged.SetName != "Royal Blood" {
		t.Fatalf("mergeSignals() set name = %q, want Royal Blood", merged.SetName)
	}
	if merged.SealedType != "booster_box" {
		t.Fatalf("mergeSignals() sealed type = %q, want booster_box", merged.SealedType)
	}
	if merged.Quantity != 24 {
		t.Fatalf("mergeSignals() quantity = %d, want 24", merged.Quantity)
	}
}

func TestSignalConflictWarningsForOCRDisagreement(t *testing.T) {
	warnings := signalConflictWarnings(
		models.Signals{SetCode: "OP10", SealedType: "starter_deck"},
		models.Signals{SetCode: "OP09", SealedType: "booster_box"},
	)

	if len(warnings) != 2 {
		t.Fatalf("signalConflictWarnings() = %v, want 2 warnings", warnings)
	}
}

func TestMergeClassificationPromotesWeakTitleToSealedProduct(t *testing.T) {
	classifier := classify.New()
	classification, textSignals := classifier.Classify("ST21", "")
	mergedSignals := mergeSignals(textSignals, models.Signals{
		SetCode:    "ST21",
		SealedType: "starter_deck",
	})

	got := mergeClassification(classification, models.ResolveRequestInput{Title: "st21"}, mergedSignals, &models.VisionAnalysisResult{
		Signals: models.Signals{SetCode: "ST21", SealedType: "starter_deck"},
	})

	if got.ProductType != "sealed_product" {
		t.Fatalf("mergeClassification() product type = %q, want sealed_product", got.ProductType)
	}
	if got.Confidence < 0.9 {
		t.Fatalf("mergeClassification() confidence = %f, want >= 0.9", got.Confidence)
	}
}

func TestMergeClassificationUsesOCRSealedTypeToUpgradeWeakText(t *testing.T) {
	classifier := classify.New()
	classification, textSignals := classifier.Classify("One Piece OP10", "")
	mergedSignals := mergeSignals(textSignals, models.Signals{SealedType: "booster_box"})

	got := mergeClassification(classification, models.ResolveRequestInput{Title: "one piece op10"}, mergedSignals, &models.VisionAnalysisResult{
		Signals: models.Signals{SealedType: "booster_box"},
	})

	if got.ProductType != "sealed_product" {
		t.Fatalf("mergeClassification() product type = %q, want sealed_product", got.ProductType)
	}
	if got.Confidence < 0.9 {
		t.Fatalf("mergeClassification() confidence = %f, want >= 0.9", got.Confidence)
	}
}

func TestResolvePrefersOCREnrichedSealedQuery(t *testing.T) {
	store := newTestStore(t)
	cardProvider := &fakeCardProvider{}
	visionProvider := &fakeVisionProvider{
		result: &models.VisionAnalysisResult{
			Signals: models.Signals{SetCode: "OP10", SealedType: "booster_box", SetName: "Royal Blood"},
		},
	}
	service := NewService(store, classify.New(), visionProvider, cardProvider)

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{
		Title:     "One Piece Booster Box",
		ImageURLs: []string{"https://example.com/box.jpg"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if response.Classification.ProductType != "sealed_product" {
		t.Fatalf("expected sealed_product, got %q", response.Classification.ProductType)
	}
	if len(cardProvider.sealedQueries) != 1 {
		t.Fatalf("expected sealed lookup once, got %d", len(cardProvider.sealedQueries))
	}
	if cardProvider.sealedQueries[0] != "OP10 booster box" {
		t.Fatalf("sealed query = %q, want %q", cardProvider.sealedQueries[0], "OP10 booster box")
	}
}

func TestResolveWarnsOnOCRConflictWithoutForcingBadMatch(t *testing.T) {
	store := newTestStore(t)
	cardProvider := &fakeCardProvider{}
	visionProvider := &fakeVisionProvider{
		result: &models.VisionAnalysisResult{
			Signals: models.Signals{SetCode: "OP09", SealedType: "booster_box"},
		},
	}
	service := NewService(store, classify.New(), visionProvider, cardProvider)

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{
		Title:     "One Piece OP10 Booster Box",
		ImageURLs: []string{"https://example.com/conflict.jpg"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if len(cardProvider.sealedQueries) != 1 {
		t.Fatalf("expected sealed lookup once, got %d", len(cardProvider.sealedQueries))
	}
	if cardProvider.sealedQueries[0] != "OP10 booster box" {
		t.Fatalf("sealed query = %q, want %q", cardProvider.sealedQueries[0], "OP10 booster box")
	}
	if len(response.Warnings) == 0 {
		t.Fatal("expected OCR conflict warning, got none")
	}
	if response.Classification.Confidence > 0.8 {
		t.Fatalf("expected lowered confidence on OCR conflict, got %f", response.Classification.Confidence)
	}
}

func TestResolvePromotesSetNameOnlyOCRIntoSealedQuery(t *testing.T) {
	store := newTestStore(t)
	cardProvider := &fakeCardProvider{}
	visionProvider := &fakeVisionProvider{
		result: &models.VisionAnalysisResult{
			Signals: models.Signals{SetName: "Royal Blood"},
		},
	}
	service := NewService(store, classify.New(), visionProvider, cardProvider)

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{
		Title:     "One Piece Box",
		ImageURLs: []string{"https://example.com/royal-blood.jpg"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if response.Classification.ProductType != "sealed_product" {
		t.Fatalf("expected sealed_product, got %q", response.Classification.ProductType)
	}
	if len(cardProvider.sealedQueries) != 1 {
		t.Fatalf("expected sealed lookup once, got %d", len(cardProvider.sealedQueries))
	}
	if cardProvider.sealedQueries[0] != "Royal Blood booster box" {
		t.Fatalf("sealed query = %q, want %q", cardProvider.sealedQueries[0], "Royal Blood booster box")
	}
}

func TestResolveSkipsGenericSealedLookupWithoutSetSignals(t *testing.T) {
	store := newTestStore(t)
	cardProvider := &fakeCardProvider{}
	service := NewService(store, classify.New(), nil, cardProvider)

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{
		Title:       "One piece booster pack",
		Description: "One booster pack unopened brand new",
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if response.Classification.ProductType != "sealed_product" {
		t.Fatalf("expected sealed_product, got %q", response.Classification.ProductType)
	}
	if len(cardProvider.sealedQueries) != 0 {
		t.Fatalf("expected no sealed lookup, got %d", len(cardProvider.sealedQueries))
	}
	if len(response.Warnings) == 0 {
		t.Fatal("expected warning for generic sealed lookup skip")
	}
}

func TestResolveCardLotLoopsOverMultipleCodes(t *testing.T) {
	store := newTestStore(t)
	provider := &fakeCardProvider{
		cardResult: &models.ProviderLookupResult{
			MarketMatch: models.MarketMatch{
				Matched:          true,
				Provider:         "card",
				Market:           "tcgplayer",
				ProductID:        "1",
				ProductName:      "Test Card",
				CardNumber:       "OP14-033",
				MarketPrice:      5.00,
				RecentMedianSale: 4.50,
				Confidence:       0.98,
			},
			ProviderResult: models.ProviderResult{Provider: "card", Market: "tcgplayer", Query: "OP14-033"},
		},
	}
	service := NewService(store, classify.New(), nil, provider)

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{
		Title: "One Piece card lot OP14-033 OP14-055 OP14-112",
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if response.Classification.ProductType != "card_lot" {
		t.Fatalf("expected card_lot, got %q", response.Classification.ProductType)
	}
	if len(provider.cardQueries) != 3 {
		t.Fatalf("expected 3 card lookups, got %d: %v", len(provider.cardQueries), provider.cardQueries)
	}
	if len(response.MarketMatches) != 3 {
		t.Fatalf("expected 3 market matches, got %d", len(response.MarketMatches))
	}
	if response.MarketMatch.MarketPrice != 15.00 {
		t.Fatalf("expected aggregated market price 15.00, got %.2f", response.MarketMatch.MarketPrice)
	}
}

func TestResolveUnsealedBoxSkipsLookup(t *testing.T) {
	store := newTestStore(t)
	provider := &fakeCardProvider{}
	service := NewService(store, classify.New(), nil, provider)

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{
		Title: "One Piece OP14 Booster Box -unsealed",
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if response.Classification.ProductType != "unknown" {
		t.Fatalf("expected unknown for unsealed box, got %q", response.Classification.ProductType)
	}
	if len(provider.cardQueries) != 0 {
		t.Fatalf("expected no card lookups for unsealed box, got %d", len(provider.cardQueries))
	}
	if len(provider.sealedQueries) != 0 {
		t.Fatalf("expected no sealed lookups for unsealed box, got %d", len(provider.sealedQueries))
	}
}

func TestProviderQueryEnrichesCardCodeWithSetName(t *testing.T) {
	query := providerQuery(
		models.ResolveRequestInput{Title: "One Piece Sabo - 500 Years in the Future OP07-118 Secret Rare"},
		models.ResolveRequestInput{Title: "one piece sabo 500 years in the future op07 118 secret rare"},
		models.Classification{ProductType: "trading_card"},
		models.Signals{CardCode: "OP07-118", SetName: "500 Years in the Future"},
	)

	if query != "OP07-118 500 Years in the Future" {
		t.Fatalf("providerQuery() = %q, want %q", query, "OP07-118 500 Years in the Future")
	}
}

func TestResolveTimesOutSlowCardLookup(t *testing.T) {
	store := newTestStore(t)
	provider := &fakeCardProvider{blockCard: true}
	service := NewService(store, classify.New(), nil, provider)

	oldResolveTimeout := resolveTimeout
	oldCardLookupTimeout := cardLookupTimeout
	resolveTimeout = 50 * time.Millisecond
	cardLookupTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		resolveTimeout = oldResolveTimeout
		cardLookupTimeout = oldCardLookupTimeout
	})

	response, err := service.Resolve(context.Background(), models.ResolveRequestInput{Title: "OP14-033 Perona"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(provider.cardQueries) != 1 {
		t.Fatalf("expected card lookup once, got %d", len(provider.cardQueries))
	}
	if len(response.Warnings) == 0 {
		t.Fatal("expected timeout warning, got none")
	}
	if got := response.Warnings[0]; got != "card lookup failed: context deadline exceeded" {
		t.Fatalf("warning = %q, want %q", got, "card lookup failed: context deadline exceeded")
	}
}

func newTestStore(t *testing.T) *db.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "resolve.db")
	sqlDB, err := db.Open("file:" + dbPath + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.ApplyMigrations(context.Background(), sqlDB, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return db.NewStore(sqlDB)
}
