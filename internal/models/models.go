package models

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
	SetCode         string   `json:"set_code,omitempty"`
	SetName         string   `json:"set_name,omitempty"`
	SealedType      string   `json:"sealed_type,omitempty"`
	LotType         string   `json:"lot_type,omitempty"`
	Variant         string   `json:"variant,omitempty"`
	Language        string   `json:"language,omitempty"`
	Quantity        int      `json:"quantity,omitempty"`
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

type ProviderResult struct {
	Provider       string
	Market         string
	Query          string
	RawPayload     string
	NormalizedJSON string
	Confidence     float64
}

type ProviderLookupResult struct {
	MarketMatch    MarketMatch
	ProviderResult ProviderResult
	Warnings       []string
}

type VisionAnalysisResult struct {
	Signals        Signals
	Reasons        []string
	Warnings       []string
	ProviderResult ProviderResult
}

type BatchResolveRequest struct {
	Requests []ResolveRequestInput `json:"requests"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type BatchItemResult struct {
	Index  int              `json:"index"`
	Result *ResolveResponse `json:"result,omitempty"`
	Error  string           `json:"error,omitempty"`
}

type BatchResolveResponse struct {
	Results []BatchItemResult `json:"results"`
}

type RequestRecord struct {
	ID               string   `json:"id"`
	Source           string   `json:"source,omitempty"`
	SourceListingID  string   `json:"source_listing_id,omitempty"`
	SourceURL        string   `json:"source_url,omitempty"`
	Title            string   `json:"title,omitempty"`
	Description      string   `json:"description,omitempty"`
	ImageURL         string   `json:"image_url,omitempty"`
	ImageURLs        []string `json:"image_urls,omitempty"`
	NormalizedTitle  string   `json:"normalized_title,omitempty"`
	NormalizedBody   string   `json:"normalized_description,omitempty"`
	NormalizedImage  string   `json:"normalized_image_url,omitempty"`
	NormalizedImages []string `json:"normalized_image_urls,omitempty"`
	Fingerprint      string   `json:"fingerprint"`
	Status           string   `json:"status"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	OriginalJSON     string   `json:"original_request_json"`
}
