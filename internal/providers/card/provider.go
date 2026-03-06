package card

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Buckelew/card/lookup"
	"github.com/Buckelew/card/tcgplayer"

	"market-api/internal/models"
)

type Provider struct {
	client     *tcgplayer.Client
	salesLimit int
}

func New(client *tcgplayer.Client, salesLimit int) *Provider {
	return &Provider{
		client:     client,
		salesLimit: salesLimit,
	}
}

func (p *Provider) LookupOnePieceCard(ctx context.Context, query string) (*models.ProviderLookupResult, error) {
	priceResult, err := lookup.Price(ctx, p.client, lookup.PriceRequest{
		Query: query,
		Game:  "onepiece",
		Limit: 30,
	})
	if err != nil {
		return nil, err
	}
	if len(priceResult.Matches) == 0 {
		return nil, nil
	}

	salesResult, salesErr := lookup.Sales(ctx, p.client, lookup.SalesRequest{
		Query:      query,
		Game:       "onepiece",
		Limit:      30,
		SalesLimit: p.salesLimit,
	})

	chosen := chooseCardMatch(query, priceResult, salesResult)
	if chosen == nil {
		return nil, nil
	}

	if int(chosen.Product.ProductID) != salesResult.ProductID {
		salesResult, salesErr = lookup.Sales(ctx, p.client, lookup.SalesRequest{
			ProductID:  int(chosen.Product.ProductID),
			SalesLimit: p.salesLimit,
			Limit:      30,
			Game:       "onepiece",
		})
	}

	marketPrice := chosen.Product.MarketPrice
	if marketPrice == 0 {
		marketPrice = firstPrice(salesResult.PricePoints)
	}

	recentMedianSale := medianSale(salesResult.LatestSales)
	confidence := confidenceForMatch(priceResult.Query, chosen)
	productID := strconv.Itoa(int(chosen.Product.ProductID))

	combinedRaw, err := marshalAny(map[string]any{
		"price": priceResult,
		"sales": salesResult,
	})
	if err != nil {
		return nil, err
	}

	normalizedJSON, err := marshalAny(map[string]any{
		"product_id":         productID,
		"product_name":       chosen.Product.ProductName,
		"market_price":       marketPrice,
		"recent_median_sale": recentMedianSale,
		"card_number":        chosen.Product.CustomAttributes.Number,
	})
	if err != nil {
		return nil, err
	}

	result := &models.ProviderLookupResult{
		MarketMatch: models.MarketMatch{
			Matched:          true,
			Provider:         "card",
			Market:           "tcgplayer",
			Query:            query,
			CardNumber:       chosen.Product.CustomAttributes.Number,
			ProductID:        productID,
			ProductName:      chosen.Product.ProductName,
			MarketPrice:      marketPrice,
			RecentMedianSale: recentMedianSale,
			Confidence:       confidence,
		},
		ProviderResult: models.ProviderResult{
			Provider:       "card",
			Market:         "tcgplayer",
			Query:          query,
			RawPayload:     combinedRaw,
			NormalizedJSON: normalizedJSON,
			Confidence:     confidence,
		},
	}

	if salesErr != nil {
		result.Warnings = append(result.Warnings, "card sales lookup failed: "+salesErr.Error())
	}
	if salesResult.LatestSalesError != "" {
		result.Warnings = append(result.Warnings, "card latest sales returned warning: "+salesResult.LatestSalesError)
	}

	return result, nil
}

func (p *Provider) LookupOnePieceSealed(ctx context.Context, query string) (*models.ProviderLookupResult, error) {
	priceResult, sealedQuery, err := lookup.SealedPrice(ctx, p.client, lookup.PriceRequest{
		Query: query,
		Game:  "onepiece",
		Limit: 40,
	})
	if err != nil {
		return nil, err
	}
	if len(priceResult.Matches) == 0 {
		return nil, nil
	}

	salesResult, _, salesErr := lookup.SealedSales(ctx, p.client, lookup.SalesRequest{
		Query:      query,
		Game:       "onepiece",
		Limit:      40,
		SalesLimit: p.salesLimit,
	})

	chosen := chooseMatch(priceResult, salesResult)
	if chosen == nil {
		return nil, nil
	}

	marketPrice := chosen.Product.MarketPrice
	if marketPrice == 0 {
		marketPrice = firstPrice(salesResult.PricePoints)
	}

	recentMedianSale := medianSale(salesResult.LatestSales)
	confidence := confidenceForSealedMatch(sealedQuery, chosen)
	productID := strconv.Itoa(int(chosen.Product.ProductID))

	combinedRaw, err := marshalAny(map[string]any{
		"price":  priceResult,
		"sales":  salesResult,
		"sealed": sealedQuery,
	})
	if err != nil {
		return nil, err
	}

	normalizedJSON, err := marshalAny(map[string]any{
		"product_id":         productID,
		"product_name":       chosen.Product.ProductName,
		"market_price":       marketPrice,
		"recent_median_sale": recentMedianSale,
		"sealed_type":        sealedQuery.SealedType,
		"set_code":           sealedQuery.SetCode,
	})
	if err != nil {
		return nil, err
	}

	result := &models.ProviderLookupResult{
		MarketMatch: models.MarketMatch{
			Matched:          true,
			Provider:         "card",
			Market:           "tcgplayer",
			Query:            query,
			ProductID:        productID,
			ProductName:      chosen.Product.ProductName,
			MarketPrice:      marketPrice,
			RecentMedianSale: recentMedianSale,
			Confidence:       confidence,
		},
		ProviderResult: models.ProviderResult{
			Provider:       "card",
			Market:         "tcgplayer",
			Query:          query,
			RawPayload:     combinedRaw,
			NormalizedJSON: normalizedJSON,
			Confidence:     confidence,
		},
	}

	if salesErr != nil {
		result.Warnings = append(result.Warnings, "sealed sales lookup failed: "+salesErr.Error())
	}
	if salesResult.LatestSalesError != "" {
		result.Warnings = append(result.Warnings, "sealed latest sales returned warning: "+salesResult.LatestSalesError)
	}

	return result, nil
}

func chooseMatch(price lookup.PriceResult, sales lookup.SalesResult) *tcgplayer.MatchCandidate {
	if sales.Match != nil {
		return sales.Match
	}
	if len(sales.ExactMatches) > 0 {
		return &sales.ExactMatches[0]
	}
	if len(price.Matches) > 0 {
		return &price.Matches[0]
	}
	return nil
}

func chooseCardMatch(query string, price lookup.PriceResult, sales lookup.SalesResult) *tcgplayer.MatchCandidate {
	if candidate := selectPreferredCardCandidate(query, price.Matches); candidate != nil {
		return candidate
	}
	return chooseMatch(price, sales)
}

func selectPreferredCardCandidate(query string, matches []tcgplayer.MatchCandidate) *tcgplayer.MatchCandidate {
	if len(matches) == 0 {
		return nil
	}

	bestIndex := 0
	bestScore := cardCandidatePreference(query, matches[0])
	for index := 1; index < len(matches); index++ {
		score := cardCandidatePreference(query, matches[index])
		if score > bestScore {
			bestIndex = index
			bestScore = score
		}
	}

	return &matches[bestIndex]
}

func cardCandidatePreference(query string, candidate tcgplayer.MatchCandidate) int {
	score := candidate.Score * 10
	queryText := normalizeCardPreferenceText(query)
	name := normalizeCardPreferenceText(candidate.Product.ProductName)
	setName := normalizeCardPreferenceText(candidate.Product.SetName)

	if queryText != "" && setName != "" && strings.Contains(queryText, setName) {
		score += 120
	}

	for _, variant := range []struct {
		token     string
		candidate string
	}{
		{token: "parallel", candidate: "parallel"},
		{token: "sp", candidate: " sp "},
		{token: "reprint", candidate: "reprint"},
		{token: "japanese", candidate: "japanese"},
	} {
		queryHas := strings.Contains(queryText, " "+variant.token+" ")
		candidateHas := strings.Contains(" "+name+" ", variant.candidate)
		if candidateHas && !queryHas {
			score -= 320
		}
		if candidateHas && queryHas {
			score += 160
		}
	}

	if !strings.Contains(" "+name+" ", " parallel ") &&
		!strings.Contains(" "+name+" ", " sp ") &&
		!strings.Contains(name, "reprint") &&
		!strings.Contains(name, "japanese") {
		score += 20
	}

	return score
}

func normalizeCardPreferenceText(value string) string {
	replacer := strings.NewReplacer("-", " ", "_", " ", "(", " ", ")", " ", "/", " ", ",", " ", ".", " ", "'", "")
	return " " + strings.Join(strings.Fields(strings.ToLower(replacer.Replace(value))), " ") + " "
}

func firstPrice(points []tcgplayer.PricePoint) float64 {
	for _, point := range points {
		if point.MarketPrice != nil && *point.MarketPrice > 0 {
			return *point.MarketPrice
		}
	}
	for _, point := range points {
		if point.ListedMedianPrice != nil && *point.ListedMedianPrice > 0 {
			return *point.ListedMedianPrice
		}
	}
	return 0
}

func medianSale(sales []tcgplayer.LatestSale) float64 {
	if len(sales) == 0 {
		return 0
	}

	values := make([]float64, 0, len(sales))
	for _, sale := range sales {
		if sale.PurchasePrice > 0 {
			values = append(values, sale.PurchasePrice)
		}
	}
	if len(values) == 0 {
		return 0
	}

	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func confidenceForMatch(query tcgplayer.NormalizedQuery, match *tcgplayer.MatchCandidate) float64 {
	if match == nil {
		return 0
	}
	if query.SetCode != "" && query.CardNumber != "" {
		normalized := strings.ToUpper(strings.TrimSpace(match.Product.CustomAttributes.Number))
		target := query.SetCode + "-" + pad3(query.CardNumber)
		if normalized == target {
			return 0.98
		}
		return 0.92
	}
	if match.Score >= 100 {
		return 0.9
	}
	return 0.75
}

func confidenceForSealedMatch(query lookup.SealedQuery, match *tcgplayer.MatchCandidate) float64 {
	if match == nil {
		return 0
	}

	confidence := 0.78
	name := strings.ToUpper(strings.TrimSpace(match.Product.ProductName + " " + match.Product.SetName))
	if query.SealedType != "" && strings.Contains(strings.ToLower(name), strings.ReplaceAll(query.SealedType, "_", " ")) {
		confidence += 0.1
	}
	if query.SetCode != "" && strings.Contains(name, query.SetCode) {
		confidence += 0.1
	}
	if confidence > 0.98 {
		return 0.98
	}
	return confidence
}

func pad3(number string) string {
	value, err := strconv.Atoi(number)
	if err != nil {
		return number
	}
	return strconv.FormatInt(int64(1000+value), 10)[1:]
}

func marshalAny(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal provider payload: %w", err)
	}
	return string(data), nil
}
