package resolve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"market-api/internal/classify"
	"market-api/internal/db"
	"market-api/internal/models"
)

type cardLookupProvider interface {
	LookupOnePieceCard(ctx context.Context, query string) (*models.ProviderLookupResult, error)
	LookupOnePieceSealed(ctx context.Context, query string) (*models.ProviderLookupResult, error)
}

type visionProvider interface {
	AnalyzeOnePiece(ctx context.Context, title, description string, imageURLs []string) (*models.VisionAnalysisResult, error)
}

type Service struct {
	store      *db.Store
	classifier *classify.Classifier
	vision     visionProvider
	card       cardLookupProvider
}

var (
	resolveTimeout        = 20 * time.Second
	visionAnalysisTimeout = 30 * time.Second
	cardLookupTimeout     = 8 * time.Second
)

func NewService(store *db.Store, classifier *classify.Classifier, vision visionProvider, card cardLookupProvider) *Service {
	return &Service{
		store:      store,
		classifier: classifier,
		vision:     vision,
		card:       card,
	}
}

func (s *Service) Resolve(ctx context.Context, input models.ResolveRequestInput) (*models.ResolveResponse, error) {
	ctx, cancel := withTimeoutCap(ctx, resolveTimeout)
	defer cancel()

	normalized := normalizeInput(input)
	fingerprint := fingerprint(normalized)

	cached, err := s.store.FindCachedResponse(ctx, fingerprint)
	if err == nil {
		return cached, nil
	}
	if err != nil && err != db.ErrNotFound {
		return nil, fmt.Errorf("check request cache: %w", err)
	}

	classification, signals := s.classifier.Classify(normalized.Title, normalized.Description)
	warnings := make([]string, 0, 2)
	providerResults := make([]models.ProviderResult, 0, 2)
	marketMatch := models.MarketMatch{
		Matched:    false,
		Provider:   "unknown",
		Market:     classification.Market,
		Confidence: 0,
	}

	if len(normalized.ImageURLs) > 0 && s.vision != nil {
		visionCtx, visionCancel := withTimeoutCap(ctx, visionAnalysisTimeout)
		visionResult, err := s.vision.AnalyzeOnePiece(visionCtx, input.Title, input.Description, normalized.ImageURLs)
		visionCancel()
		if err != nil {
			warnings = append(warnings, "image analysis failed: "+err.Error())
		} else if visionResult != nil {
			warnings = append(warnings, signalConflictWarnings(signals, visionResult.Signals)...)
			signals = mergeSignals(signals, visionResult.Signals)
			classification = mergeClassification(classification, normalized, signals, visionResult)
			warnings = append(warnings, visionResult.Warnings...)
			if visionResult.ProviderResult.Provider != "" {
				providerResults = append(providerResults, visionResult.ProviderResult)
			}
		}
	}

	var marketMatches []models.MarketMatch

	if classification.Category == "one_piece_tcg" && s.card != nil {
		marketMatch.Provider = "card"

		if shouldSkipTCGPlayerLookup(signals) {
			warnings = append(warnings, "image analysis suggests a Japanese printing; skipping TCGplayer lookup")
		} else if classification.ProductType == "card_lot" {
			codes := effectiveCardCodes(signals)
			if len(codes) == 0 {
				warnings = append(warnings, "card lot detected but no card codes found")
			} else {
				marketMatches, warnings, providerResults = s.lookupMultipleCards(ctx, input, normalized, signals, codes, warnings, providerResults)
				if len(marketMatches) > 0 {
					marketMatch = aggregateMarketMatches(marketMatches)
				}
			}
		} else if classification.ProductType == "sealed_product" && !shouldLookupSealed(signals) {
			warnings = append(warnings, "insufficient sealed set signals; skipping generic TCGplayer sealed lookup")
		} else {
			query := providerQuery(input, normalized, classification, signals)
			if query != "" {
				marketMatch.Query = query
				var lookup *models.ProviderLookupResult
				var err error
				switch classification.ProductType {
				case "sealed_product":
					lookupCtx, lookupCancel := withTimeoutCap(ctx, cardLookupTimeout)
					lookup, err = s.card.LookupOnePieceSealed(lookupCtx, query)
					lookupCancel()
				default:
					lookupCtx, lookupCancel := withTimeoutCap(ctx, cardLookupTimeout)
					lookup, err = s.card.LookupOnePieceCard(lookupCtx, query)
					lookupCancel()
				}
				if err != nil {
					warnings = append(warnings, "card lookup failed: "+err.Error())
				} else if lookup != nil {
					marketMatch = lookup.MarketMatch
					warnings = append(warnings, lookup.Warnings...)
					providerResults = append(providerResults, lookup.ProviderResult)
				}
			}
		}
	}

	requestID := uuid.NewString()
	response := &models.ResolveResponse{
		RequestID:      requestID,
		Classification: classification,
		Signals:        signals,
		MarketMatch:    marketMatch,
		MarketMatches:  marketMatches,
		Warnings:       warnings,
	}

	err = s.store.SaveResolution(ctx, db.PersistInput{
		RequestID:           requestID,
		Input:               input,
		NormalizedTitle:     normalized.Title,
		NormalizedBody:      normalized.Description,
		NormalizedImageURL:  normalized.ImageURL,
		NormalizedImageURLs: normalized.ImageURLs,
		Fingerprint:         fingerprint,
		Classification:      classification,
		Signals:             signals,
		Response:            *response,
		Status:              "completed",
		ProviderResults:     providerResults,
	})
	if err != nil {
		cached, cacheErr := s.store.FindCachedResponse(ctx, fingerprint)
		if cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("save resolution: %w", err)
	}

	return response, nil
}

func normalizeInput(input models.ResolveRequestInput) models.ResolveRequestInput {
	imageURLs := normalizeImageURLs(input.ImageURL, input.ImageURLs)

	return models.ResolveRequestInput{
		Source:          normalizeSpace(input.Source),
		SourceListingID: normalizeSpace(input.SourceListingID),
		SourceURL:       strings.TrimSpace(input.SourceURL),
		Title:           normalizeSpace(input.Title),
		Description:     normalizeSpace(input.Description),
		ImageURL:        firstOrEmpty(imageURLs),
		ImageURLs:       imageURLs,
	}
}

func normalizeSpace(value string) string {
	parts := strings.Fields(strings.ToLower(strings.TrimSpace(value)))
	return strings.Join(parts, " ")
}

func providerQuery(raw models.ResolveRequestInput, normalized models.ResolveRequestInput, classification models.Classification, signals models.Signals) string {
	if signals.CardCode != "" {
		return tradingCardProviderQuery(raw, normalized, signals)
	}
	if classification.ProductType == "sealed_product" {
		if query := sealedProviderQuery(raw, normalized, signals); query != "" {
			return query
		}
	}
	if strings.TrimSpace(raw.Title) != "" {
		return strings.TrimSpace(raw.Title)
	}
	if strings.TrimSpace(raw.Description) != "" {
		return strings.TrimSpace(raw.Description)
	}
	if normalized.Title != "" {
		return normalized.Title
	}
	return normalized.Description
}

func fingerprint(input models.ResolveRequestInput) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		input.Source,
		input.SourceListingID,
		input.SourceURL,
		input.Title,
		input.Description,
		input.ImageURL,
		strings.Join(input.ImageURLs, ","),
	}, "|")))
	return hex.EncodeToString(sum[:])
}

func normalizeImageURLs(imageURL string, imageURLs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(imageURLs)+1)

	appendURL := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	appendURL(imageURL)
	for _, value := range imageURLs {
		appendURL(value)
	}

	return out
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func mergeSignals(existing, incoming models.Signals) models.Signals {
	if existing.CardCode == "" && incoming.CardCode != "" {
		existing.CardCode = incoming.CardCode
	}
	existing.CardCodes = mergeCardCodes(existing.CardCodes, incoming.CardCodes)
	if existing.SetCode == "" && incoming.SetCode != "" {
		existing.SetCode = incoming.SetCode
	}
	if existing.SetName == "" && incoming.SetName != "" {
		existing.SetName = incoming.SetName
	}
	if existing.SealedType == "" && incoming.SealedType != "" {
		existing.SealedType = incoming.SealedType
	}
	if existing.LotType == "" && incoming.LotType != "" {
		existing.LotType = incoming.LotType
	}
	if existing.Variant == "" && incoming.Variant != "" {
		existing.Variant = incoming.Variant
	}
	if existing.Language == "" && incoming.Language != "" {
		existing.Language = incoming.Language
	}
	if existing.Quantity == 0 && incoming.Quantity > 0 {
		existing.Quantity = incoming.Quantity
	}
	if incoming.ImageLanguage != "" {
		existing.ImageLanguage = incoming.ImageLanguage
		existing.ImageConfidence = incoming.ImageConfidence
	}
	return existing
}

func mergeCardCodes(existing, incoming []string) []string {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing)+len(incoming))
	for _, code := range existing {
		if _, ok := seen[code]; !ok {
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}
	for _, code := range incoming {
		if _, ok := seen[code]; !ok {
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}
	if len(out) <= 1 {
		return nil
	}
	return out
}

func mergeClassification(existing models.Classification, normalized models.ResolveRequestInput, signals models.Signals, visionResult *models.VisionAnalysisResult) models.Classification {
	if visionResult == nil {
		return existing
	}

	if len(signals.CardCodes) > 1 && (signals.LotType != "" || existing.ProductType == "card_lot") {
		existing.ProductType = "card_lot"
		existing.Category = "one_piece_tcg"
		existing.Market = "tcgplayer"
		if existing.Confidence < 0.88 {
			existing.Confidence = 0.88
		}
	} else if len(signals.CardCodes) > 1 && existing.ProductType == "unknown" {
		existing.ProductType = "card_lot"
		existing.Category = "one_piece_tcg"
		existing.Market = "tcgplayer"
		if existing.Confidence < 0.80 {
			existing.Confidence = 0.80
		}
	} else if visionResult.Signals.CardCode != "" && len(signals.CardCodes) <= 1 {
		existing.ProductType = "trading_card"
		existing.Category = "one_piece_tcg"
		existing.Market = "tcgplayer"
		if existing.Confidence < 0.96 {
			existing.Confidence = 0.96
		}
	} else if visionResult.Signals.CardCode != "" {
		existing.Category = "one_piece_tcg"
		existing.Market = "tcgplayer"
		if existing.Confidence < 0.96 {
			existing.Confidence = 0.96
		}
	}

	if shouldPromoteToSealed(existing, normalized, signals, visionResult.Signals) {
		existing.ProductType = "sealed_product"
		existing.Category = "one_piece_tcg"
		existing.Market = "tcgplayer"
		targetConfidence := 0.84
		switch {
		case signals.SetCode != "" && effectiveSealedType(signals, normalized) != "":
			targetConfidence = 0.93
		case signals.SetName != "" && effectiveSealedType(signals, normalized) != "":
			targetConfidence = 0.88
		case signals.SealedType != "" && (signals.SetCode != "" || signals.SetName != ""):
			targetConfidence = 0.86
		}
		if existing.Confidence < targetConfidence {
			existing.Confidence = targetConfidence
		}
	}

	if visionResult.Signals.ImageLanguage == "en" && existing.Category == "one_piece_tcg" && existing.Confidence < 0.9 {
		existing.Confidence = 0.9
	}
	if hasCriticalConflict(signals, visionResult.Signals) && existing.Confidence > 0.78 {
		existing.Confidence = 0.78
	}

	for _, reason := range visionResult.Reasons {
		if !contains(existing.Reasons, reason) {
			existing.Reasons = append(existing.Reasons, reason)
		}
	}

	return existing
}

func effectiveCardCodes(signals models.Signals) []string {
	if len(signals.CardCodes) > 0 {
		return signals.CardCodes
	}
	if signals.CardCode != "" {
		return []string{signals.CardCode}
	}
	return nil
}

func (s *Service) lookupMultipleCards(
	ctx context.Context,
	raw, normalized models.ResolveRequestInput,
	signals models.Signals,
	codes []string,
	warnings []string,
	providerResults []models.ProviderResult,
) ([]models.MarketMatch, []string, []models.ProviderResult) {
	matches := make([]models.MarketMatch, 0, len(codes))
	for _, code := range codes {
		perCardSignals := models.Signals{
			CardCode: code,
			SetCode:  signals.SetCode,
			SetName:  signals.SetName,
			Language: signals.Language,
		}
		query := tradingCardProviderQuery(raw, normalized, perCardSignals)
		if query == "" {
			continue
		}
		lookupCtx, lookupCancel := withTimeoutCap(ctx, cardLookupTimeout)
		lookup, err := s.card.LookupOnePieceCard(lookupCtx, query)
		lookupCancel()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("card lookup failed for %s: %s", code, err.Error()))
			continue
		}
		if lookup == nil || !lookup.MarketMatch.Matched {
			warnings = append(warnings, fmt.Sprintf("no match found for %s", code))
			continue
		}
		matches = append(matches, lookup.MarketMatch)
		providerResults = append(providerResults, lookup.ProviderResult)
	}
	return matches, warnings, providerResults
}

func aggregateMarketMatches(matches []models.MarketMatch) models.MarketMatch {
	if len(matches) == 0 {
		return models.MarketMatch{Matched: false, Provider: "card", Market: "tcgplayer"}
	}

	totalMarketPrice := 0.0
	totalRecentSale := 0.0
	minConfidence := 1.0
	names := make([]string, 0, len(matches))

	for _, m := range matches {
		totalMarketPrice += m.MarketPrice
		totalRecentSale += m.RecentMedianSale
		if m.Confidence < minConfidence {
			minConfidence = m.Confidence
		}
		if m.ProductName != "" {
			names = append(names, m.CardNumber)
		}
	}

	return models.MarketMatch{
		Matched:          true,
		Provider:         "card",
		Market:           "tcgplayer",
		ProductName:      fmt.Sprintf("%d cards: %s", len(matches), strings.Join(names, ", ")),
		MarketPrice:      totalMarketPrice,
		RecentMedianSale: totalRecentSale,
		Confidence:       minConfidence,
	}
}

func shouldSkipTCGPlayerLookup(signals models.Signals) bool {
	return signals.ImageLanguage == "jp" && signals.ImageConfidence >= 0.7
}

func withTimeoutCap(ctx context.Context, max time.Duration) (context.Context, context.CancelFunc) {
	if max <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= max {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, max)
}

func shouldLookupSealed(signals models.Signals) bool {
	return signals.SetCode != "" || signals.SetName != ""
}

func tradingCardProviderQuery(raw models.ResolveRequestInput, normalized models.ResolveRequestInput, signals models.Signals) string {
	parts := []string{signals.CardCode}
	if signals.SetName != "" {
		parts = append(parts, signals.SetName)
	}
	// Prefer image-detected variant over text-based hint
	if hint := variantHint(signals, raw, normalized); hint != "" {
		parts = append(parts, hint)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// variantHint returns a query hint for variant matching.
// Prefers the vision-detected variant from the image, falls back to text heuristics.
func variantHint(signals models.Signals, raw, normalized models.ResolveRequestInput) string {
	if signals.Variant != "" && signals.Variant != "base" {
		switch signals.Variant {
		case "parallel":
			return "alternate art"
		case "sp":
			return "sp"
		case "manga":
			return "manga"
		case "reprint":
			return "reprint"
		case "promo":
			return "promo"
		}
	}
	return variantQueryHint(raw, normalized)
}

func sealedProviderQuery(raw models.ResolveRequestInput, normalized models.ResolveRequestInput, signals models.Signals) string {
	sealedType := effectiveSealedType(signals, normalized)

	switch {
	case signals.SetCode != "" && sealedType != "":
		return strings.TrimSpace(signals.SetCode + " " + strings.ReplaceAll(sealedType, "_", " "))
	case signals.SetName != "" && sealedType != "":
		return strings.TrimSpace(signals.SetName + " " + strings.ReplaceAll(sealedType, "_", " "))
	case signals.SetCode != "" && signals.SetName != "":
		return strings.TrimSpace(signals.SetCode + " " + signals.SetName)
	case signals.SetCode != "":
		return signals.SetCode
	case signals.SetName != "":
		if packaging := packagingQueryHint(raw, normalized); packaging != "" {
			return strings.TrimSpace(signals.SetName + " " + packaging)
		}
		return signals.SetName
	default:
		return ""
	}
}

func shouldPromoteToSealed(existing models.Classification, normalized models.ResolveRequestInput, signals models.Signals, visionSignals models.Signals) bool {
	if existing.ProductType == "sealed_product" {
		return true
	}
	if visionSignals.CardCode != "" {
		return false
	}
	if signals.CardCode != "" {
		return false
	}

	sealedType := effectiveSealedType(signals, normalized)
	if sealedType == "" {
		return false
	}

	if signals.SetCode == "" && signals.SetName == "" {
		return false
	}

	return existing.Category == "one_piece_tcg" || listingLooksOnePiece(normalized, signals)
}

func effectiveSealedType(signals models.Signals, normalized models.ResolveRequestInput) string {
	if signals.SealedType != "" {
		return signals.SealedType
	}
	if signals.SetCode == "" && signals.SetName == "" {
		return ""
	}
	return inferGenericSealedType(normalized.Title + " " + normalized.Description)
}

func packagingQueryHint(raw models.ResolveRequestInput, normalized models.ResolveRequestInput) string {
	if sealedType := inferGenericSealedType(strings.TrimSpace(raw.Title + " " + raw.Description)); sealedType != "" {
		return strings.ReplaceAll(sealedType, "_", " ")
	}
	if sealedType := inferGenericSealedType(normalized.Title + " " + normalized.Description); sealedType != "" {
		return strings.ReplaceAll(sealedType, "_", " ")
	}
	return ""
}

func variantQueryHint(raw models.ResolveRequestInput, normalized models.ResolveRequestInput) string {
	combined := " " + strings.ToLower(strings.TrimSpace(strings.Join([]string{
		raw.Title,
		raw.Description,
		normalized.Title,
		normalized.Description,
	}, " "))) + " "

	switch {
	case strings.Contains(combined, " reprint "):
		return "reprint"
	case strings.Contains(combined, " alt art ") || strings.Contains(combined, " alternate art "):
		return "alternate art"
	case strings.Contains(combined, " parallel "):
		return "parallel"
	case strings.Contains(combined, " sp "):
		return "sp"
	case strings.Contains(combined, " japanese ") || strings.Contains(combined, " jp "):
		return "japanese"
	default:
		return ""
	}
}

func inferGenericSealedType(text string) string {
	lowered := " " + strings.ToLower(strings.TrimSpace(text)) + " "

	switch {
	case strings.Contains(lowered, " gift collection "):
		return "gift_collection"
	case strings.Contains(lowered, " collection box "):
		return "collection_box"
	case strings.Contains(lowered, " double pack "):
		return "double_pack"
	case strings.Contains(lowered, " starter deck ") || strings.Contains(lowered, " deck "):
		return "starter_deck"
	case strings.Contains(lowered, " booster pack ") || strings.Contains(lowered, " pack "):
		return "booster_pack"
	case strings.Contains(lowered, " booster box ") || strings.Contains(lowered, " display box ") || strings.Contains(lowered, " box "):
		return "booster_box"
	default:
		return ""
	}
}

func listingLooksOnePiece(normalized models.ResolveRequestInput, signals models.Signals) bool {
	if signals.SetCode != "" {
		return true
	}
	combined := " " + normalized.Title + " " + normalized.Description + " "
	return strings.Contains(combined, " one piece ")
}

func signalConflictWarnings(existing, incoming models.Signals) []string {
	warnings := make([]string, 0, 3)
	appendConflict := func(label, left, right string) {
		if left == "" || right == "" || strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) {
			return
		}
		warnings = append(warnings, fmt.Sprintf("text %s %s conflicts with OCR %s %s", label, left, label, right))
	}

	appendConflict("set code", existing.SetCode, incoming.SetCode)
	appendConflict("set name", existing.SetName, incoming.SetName)
	appendConflict("sealed type", existing.SealedType, incoming.SealedType)
	appendConflict("language", existing.Language, incoming.Language)
	if existing.Quantity > 0 && incoming.Quantity > 0 && existing.Quantity != incoming.Quantity {
		warnings = append(warnings, fmt.Sprintf("text quantity %d conflicts with OCR quantity %d", existing.Quantity, incoming.Quantity))
	}

	return warnings
}

func hasCriticalConflict(existing, incoming models.Signals) bool {
	return conflictsOn(existing.SetCode, incoming.SetCode) || conflictsOn(existing.SealedType, incoming.SealedType)
}

func conflictsOn(existing, incoming string) bool {
	if strings.TrimSpace(existing) == "" || strings.TrimSpace(incoming) == "" {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(incoming))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
