package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"market-api/internal/classify"
	"market-api/internal/models"
)

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

type searchItem struct {
	ID string `json:"id"`
}

type listingDetail struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Photos      []listingPhoto `json:"photos"`
}

type listingPhoto struct {
	ImageURL string `json:"imageUrl"`
}

type fixtureFile struct {
	GeneratedAt string        `json:"generated_at"`
	Queries     []string      `json:"queries"`
	Cases       []fixtureCase `json:"cases"`
}

type fixtureCase struct {
	ListingID           string                     `json:"listing_id"`
	ListingURL          string                     `json:"listing_url"`
	SourceQuery         string                     `json:"source_query"`
	ExpectedCardCode    string                     `json:"expected_card_code"`
	ExpectedLanguage    string                     `json:"expected_language"`
	ListingKind         string                     `json:"listing_kind"`
	OriginalTitle       string                     `json:"original_title,omitempty"`
	OriginalDescription string                     `json:"original_description,omitempty"`
	RedactedTitle       string                     `json:"redacted_title,omitempty"`
	RedactedDescription string                     `json:"redacted_description,omitempty"`
	ImageURLs           []string                   `json:"image_urls"`
	RequestRedacted     models.ResolveRequestInput `json:"request_redacted"`
	RequestImageOnly    models.ResolveRequestInput `json:"request_image_only"`
}

func main() {
	var queries stringListFlag
	var outPath string
	var limit int
	var maxCases int

	flag.Var(&queries, "query", "Mercari query to search with cari (repeatable)")
	flag.StringVar(&outPath, "out", filepath.Join("testdata", "one_piece_image_cases.json"), "output fixture file")
	flag.IntVar(&limit, "limit", 5, "per-query listing limit")
	flag.IntVar(&maxCases, "max-cases", 24, "maximum cases to emit")
	flag.Parse()

	if len(queries) == 0 {
		queries = defaultQueries()
	}
	if limit < 1 {
		fail("limit must be >= 1")
	}
	if maxCases < 1 {
		fail("max-cases must be >= 1")
	}

	cases, err := collectCases(queries, limit, maxCases)
	if err != nil {
		fail(err.Error())
	}

	payload := fixtureFile{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Queries:     []string(queries),
		Cases:       cases,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fail(fmt.Sprintf("marshal fixture file: %v", err))
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fail(fmt.Sprintf("mkdir %s: %v", filepath.Dir(outPath), err))
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		fail(fmt.Sprintf("write %s: %v", outPath, err))
	}

	fmt.Fprintf(os.Stderr, "wrote %d cases to %s\n", len(cases), outPath)
}

func collectCases(queries []string, limit, maxCases int) ([]fixtureCase, error) {
	seenIDs := map[string]struct{}{}
	out := make([]fixtureCase, 0, maxCases)

	for _, query := range queries {
		searchResults, err := cariSearch(query, limit)
		if err != nil {
			return nil, err
		}

		for _, item := range searchResults {
			if len(out) >= maxCases {
				return out, nil
			}
			if item.ID == "" {
				continue
			}
			if _, ok := seenIDs[item.ID]; ok {
				continue
			}

			detail, err := cariGetListing(item.ID)
			if err != nil {
				continue
			}

			cardCode := classify.ExtractOnePieceCardCode(detail.Name + "\n" + detail.Description)
			if cardCode == "" {
				continue
			}

			imageURLs := make([]string, 0, len(detail.Photos))
			for _, photo := range detail.Photos {
				if trimmed := strings.TrimSpace(photo.ImageURL); trimmed != "" {
					imageURLs = append(imageURLs, trimmed)
				}
			}
			if len(imageURLs) == 0 {
				continue
			}

			redactedTitle := redactCardCode(detail.Name, cardCode)
			redactedDescription := redactCardCode(detail.Description, cardCode)
			fx := fixtureCase{
				ListingID:           detail.ID,
				ListingURL:          "https://www.mercari.com/us/item/" + detail.ID + "/",
				SourceQuery:         query,
				ExpectedCardCode:    cardCode,
				ExpectedLanguage:    inferLanguage(detail.Name, detail.Description, query),
				ListingKind:         inferListingKind(detail.Name, detail.Description),
				OriginalTitle:       detail.Name,
				OriginalDescription: detail.Description,
				RedactedTitle:       redactedTitle,
				RedactedDescription: redactedDescription,
				ImageURLs:           imageURLs,
				RequestRedacted: models.ResolveRequestInput{
					Source:          "mercari",
					SourceListingID: detail.ID,
					SourceURL:       "https://www.mercari.com/us/item/" + detail.ID + "/",
					Title:           redactedTitle,
					Description:     redactedDescription,
					ImageURLs:       imageURLs,
				},
				RequestImageOnly: models.ResolveRequestInput{
					Source:          "mercari",
					SourceListingID: detail.ID,
					SourceURL:       "https://www.mercari.com/us/item/" + detail.ID + "/",
					ImageURLs:       imageURLs,
				},
			}

			out = append(out, fx)
			seenIDs[detail.ID] = struct{}{}
		}
	}

	return out, nil
}

func cariSearch(query string, limit int) ([]searchItem, error) {
	output, err := execCari("search", query, "--limit", fmt.Sprintf("%d", limit), "--sort-by", "newest", "--json")
	if err != nil {
		return nil, fmt.Errorf("cari search %q: %w", query, err)
	}

	var items []searchItem
	if err := json.Unmarshal(output, &items); err != nil {
		return nil, fmt.Errorf("decode cari search %q: %w", query, err)
	}
	return items, nil
}

func cariGetListing(id string) (listingDetail, error) {
	output, err := execCari("get-listing", id, "--json")
	if err != nil {
		return listingDetail{}, fmt.Errorf("cari get-listing %q: %w", id, err)
	}

	var listings []listingDetail
	if err := json.Unmarshal(output, &listings); err != nil {
		return listingDetail{}, fmt.Errorf("decode cari get-listing %q: %w", id, err)
	}
	if len(listings) == 0 {
		return listingDetail{}, fmt.Errorf("listing %q not found", id)
	}
	return listings[0], nil
}

func execCari(args ...string) ([]byte, error) {
	cmd := exec.Command("cari", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func redactCardCode(input, cardCode string) string {
	if strings.TrimSpace(input) == "" || strings.TrimSpace(cardCode) == "" {
		return strings.TrimSpace(input)
	}

	variants := cardCodeVariants(cardCode)
	replacer := strings.NewReplacer(
		"(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ",
		":", " ", ";", " ", "/", " ", "\\", " ", ",", " ", "*", " ",
		"|", " ", "_", " ", "-", " ",
	)

	out := input
	for _, variant := range variants {
		out = strings.ReplaceAll(out, variant, " ")
		out = strings.ReplaceAll(out, strings.ToLower(variant), " ")
		out = strings.ReplaceAll(out, strings.ToUpper(variant), " ")
	}

	out = replacer.Replace(out)
	out = strings.Join(strings.Fields(out), " ")
	return strings.TrimSpace(out)
}

func cardCodeVariants(cardCode string) []string {
	upper := strings.ToUpper(strings.TrimSpace(cardCode))
	if upper == "" {
		return nil
	}

	variants := []string{upper}
	left, right, hasHyphen := strings.Cut(upper, "-")
	if hasHyphen {
		variants = append(variants, strings.ReplaceAll(upper, "-", " "))
		variants = append(variants, strings.ReplaceAll(upper, "-", ""))
		variants = append(variants, strings.ReplaceAll(upper, "-", "_"))
		if left == "P" {
			variants = append(variants, "P "+right)
		} else {
			prefix, setNumber := splitSetCode(left)
			if prefix != "" && setNumber != "" {
				variants = append(variants,
					prefix+" "+setNumber+"-"+right,
					prefix+" "+setNumber+" "+right,
					prefix+setNumber+" "+right,
					prefix+"-"+setNumber+"-"+right,
				)
			}
		}
	}

	dedup := make([]string, 0, len(variants))
	seen := map[string]struct{}{}
	for _, variant := range variants {
		variant = strings.TrimSpace(variant)
		if variant == "" {
			continue
		}
		if _, ok := seen[variant]; ok {
			continue
		}
		seen[variant] = struct{}{}
		dedup = append(dedup, variant)
	}

	slices.Sort(dedup)
	return dedup
}

func inferLanguage(title, description, sourceQuery string) string {
	combined := normalizeMetadata(title + " " + description + " " + sourceQuery)
	switch {
	case strings.Contains(combined, " japanese ") || strings.Contains(combined, " jp ") || strings.Contains(combined, " japan ") || strings.Contains(combined, " 日本 "):
		return "jp"
	case strings.Contains(combined, " english ") || strings.Contains(combined, " english edition "):
		return "en"
	default:
		return "unknown"
	}
}

func inferListingKind(title, description string) string {
	combined := strings.ToLower(title + " " + description)
	switch {
	case strings.Contains(combined, "lot") || strings.Contains(combined, "bundle") || strings.Contains(combined, "playset"):
		return "lot"
	case strings.Contains(combined, "box") || strings.Contains(combined, "booster") || strings.Contains(combined, "deck") || strings.Contains(combined, "sealed"):
		return "sealed"
	default:
		return "single"
	}
}

func defaultQueries() []string {
	return []string{
		"one piece op",
		"one piece st",
		"one piece prb english",
		"one piece english op",
		"one piece japanese op",
		"one piece st english",
		"one piece magazine japanese",
		"one piece card lot",
		"japanese one piece card lot",
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func splitSetCode(input string) (string, string) {
	index := 0
	for index < len(input) && input[index] >= 'A' && input[index] <= 'Z' {
		index++
	}
	if index == 0 || index == len(input) {
		return "", ""
	}
	return input[:index], input[index:]
}

func normalizeMetadata(input string) string {
	replacer := strings.NewReplacer(
		"(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ",
		":", " ", ";", " ", "/", " ", "\\", " ", ",", " ", "*", " ",
		"|", " ", "_", " ", "-", " ", ".", " ", "#", " ",
	)
	return " " + strings.Join(strings.Fields(strings.ToLower(replacer.Replace(input))), " ") + " "
}
