package vinyl

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"discogs/api"

	"market-api/internal/models"
)

type Provider struct {
	client *api.Client
}

func New(client *api.Client) *Provider {
	return &Provider{client: client}
}

func (p *Provider) LookupVinyl(ctx context.Context, query string) (*models.ProviderLookupResult, error) {
	searchResult, err := p.client.Search(ctx, api.SearchOptions{
		Query:   query,
		Type:    "release",
		Format:  "Vinyl",
		PerPage: 5,
	})
	if err != nil {
		return nil, fmt.Errorf("discogs search: %w", err)
	}
	if len(searchResult.Results) == 0 {
		return nil, nil
	}

	match := searchResult.Results[0]
	releaseID := match.ID

	release, err := p.client.Release(ctx, releaseID)
	if err != nil {
		return nil, fmt.Errorf("discogs release %d: %w", releaseID, err)
	}

	var marketPrice float64

	suggestions, err := p.client.PriceSuggestions(ctx, releaseID)
	if err == nil && suggestions != nil {
		if vgPlus, ok := (*suggestions)["Very Good Plus (VG+)"]; ok && vgPlus.Value > 0 {
			marketPrice = vgPlus.Value
		}
	}

	if marketPrice == 0 && release.LowestPrice != nil {
		marketPrice = *release.LowestPrice
	}

	artist := release.ArtistsSort
	productName := artist + " - " + release.Title
	productID := strconv.Itoa(releaseID)
	confidence := titleOverlapConfidence(query, match.Title)

	marketMatch := models.MarketMatch{
		Matched:     marketPrice > 0,
		Provider:    "vinyl",
		Market:      "discogs",
		Query:       query,
		ProductID:   productID,
		ProductName: productName,
		MarketPrice: marketPrice,
		Confidence:  confidence,
	}

	rawPayload, _ := json.Marshal(map[string]any{
		"search_match": match,
		"release":      release,
		"suggestions":  suggestions,
	})
	normalizedPayload, _ := json.Marshal(map[string]any{
		"release_id":   releaseID,
		"artist":       artist,
		"title":        release.Title,
		"market_price": marketPrice,
		"num_for_sale": release.NumForSale,
	})

	providerResult := models.ProviderResult{
		Provider:       "vinyl",
		Market:         "discogs",
		Query:          query,
		RawPayload:     string(rawPayload),
		NormalizedJSON: string(normalizedPayload),
		Confidence:     confidence,
	}

	return &models.ProviderLookupResult{
		MarketMatch:    marketMatch,
		ProviderResult: providerResult,
	}, nil
}

func titleOverlapConfidence(query, matchTitle string) float64 {
	queryWords := strings.Fields(strings.ToLower(query))
	titleWords := strings.Fields(strings.ToLower(matchTitle))
	if len(queryWords) == 0 {
		return 0.5
	}

	titleSet := make(map[string]struct{}, len(titleWords))
	for _, w := range titleWords {
		titleSet[w] = struct{}{}
	}

	hits := 0
	for _, w := range queryWords {
		if _, ok := titleSet[w]; ok {
			hits++
		}
	}

	overlap := float64(hits) / float64(len(queryWords))
	switch {
	case overlap >= 0.8:
		return 0.95
	case overlap >= 0.5:
		return 0.80
	case overlap >= 0.3:
		return 0.65
	default:
		return 0.50
	}
}
