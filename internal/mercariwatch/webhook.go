package mercariwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func IsJapaneseListing(response *ResolveResponse) bool {
	if response == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(response.Signals.ImageLanguage), "jp") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(response.Signals.Language), "jp") {
		return true
	}
	for _, warning := range response.Warnings {
		lowered := strings.ToLower(strings.TrimSpace(warning))
		if strings.Contains(lowered, "japanese printing") || strings.Contains(lowered, "skipping tcgplayer lookup") {
			return true
		}
	}
	return false
}

func PostWebhook(ctx context.Context, client *http.Client, webhookURL string, payload map[string]any) error {
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return nil
	}
	if client == nil {
		client = &http.Client{}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

func BuildFindingPayload(username, label string, listing *Listing, response *ResolveResponse, resolveErr string) map[string]any {
	title := "Mercari Finding"
	url := ""
	imageURL := ""
	fields := make([]map[string]any, 0, 10)

	if listing != nil {
		if strings.TrimSpace(listing.Name) != "" {
			title = listing.Name
		}
		url = MercariListingURL(listing.ID)
		for _, photo := range listing.Photos {
			if strings.TrimSpace(photo.ImageURL) != "" {
				imageURL = strings.TrimSpace(photo.ImageURL)
				break
			}
		}
		fields = append(fields,
			field("Mercari Price", dollars(float64(listing.Price)/100.0), true),
			field("Shipping", shippingText(listing.ShippingClass.Fee), true),
		)
	}

	if strings.TrimSpace(label) != "" {
		fields = append(fields, field("Source", label, false))
	}

	if response != nil {
		fields = append(fields,
			field("Classification", classificationText(response), true),
			field("Card Code", fallback(response.Signals.CardCode, "none"), true),
			field("Match", matchText(response.MarketMatch), false),
		)
		if response.MarketMatch.Matched {
			fields = append(fields,
				field("Market Price", dollars(response.MarketMatch.MarketPrice), true),
				field("Recent Sale", dollars(response.MarketMatch.RecentMedianSale), true),
			)
		}
		if len(response.MarketMatches) > 0 {
			fields = append(fields, field("Card Matches", multiMatchText(response.MarketMatches), false))
		}
		if warnings := warningsText(response.Warnings); warnings != "" {
			fields = append(fields, field("Warnings", warnings, false))
		}
	}

	if strings.TrimSpace(resolveErr) != "" {
		fields = append(fields, field("Resolve Error", resolveErr, false))
	}

	embed := map[string]any{
		"title":  title,
		"url":    url,
		"fields": fields,
	}
	if imageURL != "" {
		embed["image"] = map[string]any{"url": imageURL}
	}

	return map[string]any{
		"username": fallback(username, "Mercari Market Bot"),
		"embeds":   []map[string]any{embed},
	}
}

func BuildDealPayload(username, label string, listing *Listing, response *ResolveResponse, score DealScore) map[string]any {
	title := "Mercari Deal"
	url := ""
	imageURL := ""
	fields := []map[string]any{
		field("Expected Sale", dollars(score.ExpectedSaleUSD), true),
		field("Total Cost", dollars(score.TotalCostUSD), true),
		field("Profit", dollars(score.ProfitUSD), true),
		field("ROI", fmt.Sprintf("%.1f%%", score.ROI*100), true),
		field("Confidence", fmt.Sprintf("%.2f", score.Confidence), true),
		field("Reasons", bulletList(score.Reasons), false),
	}

	if listing != nil {
		if strings.TrimSpace(listing.Name) != "" {
			title = listing.Name
		}
		url = MercariListingURL(listing.ID)
		for _, photo := range listing.Photos {
			if strings.TrimSpace(photo.ImageURL) != "" {
				imageURL = strings.TrimSpace(photo.ImageURL)
				break
			}
		}
		fields = append([]map[string]any{
			field("Mercari Price", dollars(float64(listing.Price)/100.0), true),
			field("Shipping", shippingText(listing.ShippingClass.Fee), true),
		}, fields...)
	}

	if strings.TrimSpace(label) != "" {
		fields = append(fields, field("Source", label, false))
	}
	if response != nil {
		if len(response.MarketMatches) > 0 {
			fields = append(fields, field("Card Matches", multiMatchText(response.MarketMatches), false))
		} else {
			fields = append(fields,
				field("Card Match", fallback(response.MarketMatch.ProductName, "Unknown"), false),
				field("TCGplayer", tcgPlayerLink(response.MarketMatch.ProductID), false),
			)
		}
		fields = append(fields, field("Card Code", fallback(response.Signals.CardCode, "none"), true))
	}

	embed := map[string]any{
		"title":  title,
		"url":    url,
		"fields": fields,
	}
	if imageURL != "" {
		embed["image"] = map[string]any{"url": imageURL}
	}

	return map[string]any{
		"username": fallback(username, "Mercari Deals Bot"),
		"embeds":   []map[string]any{embed},
	}
}

func field(name, value string, inline bool) map[string]any {
	return map[string]any{
		"name":   name,
		"value":  fallback(value, "none"),
		"inline": inline,
	}
}

func classificationText(response *ResolveResponse) string {
	if response == nil {
		return "unknown"
	}
	category := fallback(response.Classification.Category, "unknown")
	return fmt.Sprintf("%s (%.2f)", category, response.Classification.Confidence)
}

func multiMatchText(matches []MarketMatch) string {
	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		name := fallback(m.CardNumber, m.ProductName)
		lines = append(lines, fmt.Sprintf("- %s: %s (mkt) / %s (sale)", name, dollars(m.MarketPrice), dollars(m.RecentMedianSale)))
	}
	return strings.Join(lines, "\n")
}

func matchText(match MarketMatch) string {
	if !match.Matched {
		return "No match"
	}
	name := fallback(match.ProductName, "Unknown")
	return fmt.Sprintf("%s (%.2f)", name, match.Confidence)
}

func warningsText(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	return bulletList(warnings)
}

func bulletList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, "- "+item)
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, "\n")
}

func dollars(v float64) string {
	if v <= 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", v)
}

func shippingText(fee *int) string {
	if fee == nil {
		return "unknown"
	}
	return dollars(float64(*fee) / 100.0)
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func tcgPlayerLink(productID string) string {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return "No product link"
	}
	return "https://www.tcgplayer.com/product/" + productID
}
