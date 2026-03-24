package mercariwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CariClient struct {
	Bin            string
	CommandTimeout time.Duration
}

type SearchPhoto struct {
	Thumbnail string `json:"thumbnail"`
	ImageURL  string `json:"imageUrl"`
}

type SearchShippingClass struct {
	Fee *int `json:"fee"`
}

type SearchSeller struct {
	ID any `json:"id"`
}

type SearchItem struct {
	ID                      string               `json:"id"`
	Name                    string               `json:"name"`
	Status                  string               `json:"status"`
	Price                   int                  `json:"price"`
	ShippingClass           SearchShippingClass  `json:"shippingClass"`
	Photos                  []SearchPhoto        `json:"photos"`
	Seller                  SearchSeller         `json:"seller"`
	ItemDecorationRectangle *DecorationRectangle `json:"itemDecorationRectangle"`
}

type DecorationRectangle struct {
	Text string `json:"text"`
}

type SavedQueryScanResult struct {
	SavedQueryID   int          `json:"savedQueryId"`
	SavedQueryName string       `json:"savedQueryName"`
	NewItemCount   int          `json:"newItemCount"`
	IsNew          bool         `json:"isNew"`
	Items          []SearchItem `json:"items"`
}

type ListingShippingClass struct {
	Fee *int `json:"fee"`
}

type ListingPhoto struct {
	Thumbnail string `json:"thumbnail"`
	ImageURL  string `json:"imageUrl"`
}

type Listing struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Status        string               `json:"status"`
	Price         int                  `json:"price"`
	ShippingClass ListingShippingClass `json:"shippingClass"`
	Description   string               `json:"description"`
	Created       int64                `json:"created"`
	Updated       int64                `json:"updated"`
	Photos        []ListingPhoto       `json:"photos"`
	Seller        struct {
		ID any `json:"id"`
	} `json:"seller"`
}

type ResolveRequestInput struct {
	Source          string   `json:"source,omitempty"`
	SourceListingID string   `json:"source_listing_id,omitempty"`
	SourceURL       string   `json:"source_url,omitempty"`
	Title           string   `json:"title,omitempty"`
	Description     string   `json:"description,omitempty"`
	ImageURL        string   `json:"image_url,omitempty"`
	ImageURLs       []string `json:"image_urls,omitempty"`
	Category        string   `json:"category,omitempty"`
}

type Classification struct {
	ProductType string   `json:"product_type"`
	Category    string   `json:"category"`
	Market      string   `json:"market"`
	Confidence  float64  `json:"confidence"`
	Reasons     []string `json:"reasons"`
}

type Signals struct {
	CardCode        string   `json:"card_code,omitempty"`
	CardCodes       []string `json:"card_codes,omitempty"`
	LotType         string   `json:"lot_type,omitempty"`
	Language        string   `json:"language,omitempty"`
	ImageLanguage   string   `json:"image_language,omitempty"`
	ImageConfidence float64  `json:"image_confidence,omitempty"`
}

type MarketMatch struct {
	Matched          bool    `json:"matched"`
	Provider         string  `json:"provider"`
	Market           string  `json:"market"`
	Query            string  `json:"query,omitempty"`
	CardNumber       string  `json:"card_number,omitempty"`
	ProductID        string  `json:"product_id,omitempty"`
	ProductName      string  `json:"product_name,omitempty"`
	MarketPrice      float64 `json:"market_price,omitempty"`
	RecentMedianSale float64 `json:"recent_median_sale,omitempty"`
	Confidence       float64 `json:"confidence"`
}

type ResolveResponse struct {
	RequestID      string         `json:"request_id"`
	Classification Classification `json:"classification"`
	Signals        Signals        `json:"signals"`
	MarketMatch    MarketMatch    `json:"market_match"`
	MarketMatches  []MarketMatch  `json:"market_matches,omitempty"`
	Warnings       []string       `json:"warnings"`
}

type BatchItemResult struct {
	Index  int              `json:"index"`
	Result *ResolveResponse `json:"result,omitempty"`
	Error  string           `json:"error,omitempty"`
}

type BatchResolveResponse struct {
	Results []BatchItemResult `json:"results"`
}

type MarketAPIClient struct {
	BaseURL   string
	Client    *http.Client
	AuthToken string
}

func (c *CariClient) ScanSavedQueries(ctx context.Context, onlyNew bool, limit int) ([]SavedQueryScanResult, error) {
	args := []string{"saved-queries", "scan", "--json"}
	if onlyNew {
		args = append(args, "--new")
	}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}

	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var results []SavedQueryScanResult
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, fmt.Errorf("parse cari saved-queries scan json: %w", err)
	}
	return results, nil
}

func (c *CariClient) SearchDeals(ctx context.Context, term string, limit int) ([]SearchItem, error) {
	args := []string{"search", term, "--deals", "--for-sale", "--sort-by", "deals", "--json"}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}

	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var items []SearchItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse cari search json: %w", err)
	}
	return items, nil
}

func (c *CariClient) GetListing(ctx context.Context, listingID string) (*Listing, error) {
	out, err := c.run(ctx, "get-listing", listingID, "--json")
	if err != nil {
		return nil, err
	}

	var items []Listing
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse cari get-listing json: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("listing %q not found", listingID)
	}
	return &items[0], nil
}

func (c *CariClient) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := strings.TrimSpace(c.Bin)
	if bin == "" {
		bin = "cari"
	}

	timeout := c.CommandTimeout
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s failed: %w (%s)", bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (c *MarketAPIClient) ResolveBatch(ctx context.Context, requests []ResolveRequestInput) ([]BatchItemResult, error) {
	body, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		return nil, fmt.Errorf("marshal batch resolve request: %w", err)
	}

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18080"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/resolve/batch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.AuthToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-API-Key", token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call market-api batch resolve: %w", err)
	}
	defer resp.Body.Close()

	var parsed BatchResolveResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode market-api batch response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("market-api batch resolve returned status %d", resp.StatusCode)
	}
	return parsed.Results, nil
}

func MercariListingURL(id string) string {
	return "https://www.mercari.com/us/item/" + strings.TrimSpace(id) + "/"
}

func ListingImageURLs(listing *Listing) []string {
	if listing == nil {
		return nil
	}
	out := make([]string, 0, len(listing.Photos))
	for _, photo := range listing.Photos {
		url := strings.TrimSpace(photo.ImageURL)
		if url != "" {
			out = append(out, url)
		}
	}
	return out
}

func SearchItemImageURL(item SearchItem) string {
	for _, photo := range item.Photos {
		url := strings.TrimSpace(photo.ImageURL)
		if url != "" {
			return url
		}
	}
	return ""
}

func ListingFromSearchItem(item SearchItem) *Listing {
	listing := &Listing{
		ID:          strings.TrimSpace(item.ID),
		Name:        strings.TrimSpace(item.Name),
		Status:      strings.TrimSpace(item.Status),
		Price:       item.Price,
		Description: "",
	}
	listing.ShippingClass.Fee = item.ShippingClass.Fee
	if imageURL := SearchItemImageURL(item); imageURL != "" {
		listing.Photos = []ListingPhoto{{ImageURL: imageURL}}
	}
	listing.Seller.ID = item.Seller.ID
	return listing
}
