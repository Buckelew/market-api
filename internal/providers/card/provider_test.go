package card

import (
	"testing"

	"github.com/Buckelew/card/tcgplayer"
)

func TestSelectPreferredCardCandidatePrefersBasePrinting(t *testing.T) {
	t.Parallel()

	candidates := []tcgplayer.MatchCandidate{
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   655082,
				ProductName: "Sabo - OP07-118 (SP)",
				SetName:     "Carrying On His Will",
			},
			Score: 153,
		},
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   656224,
				ProductName: "Sabo - OP07-118 (Reprint)",
				SetName:     "Premium Booster -The Best- Vol. 2",
			},
			Score: 153,
		},
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   545919,
				ProductName: "Sabo (Parallel)",
				SetName:     "500 Years in the Future",
			},
			Score: 135,
		},
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   545918,
				ProductName: "Sabo",
				SetName:     "500 Years in the Future",
			},
			Score: 135,
		},
	}

	chosen := selectPreferredCardCandidate("OP07-118 500 Years in the Future", candidates)
	if chosen == nil {
		t.Fatal("selectPreferredCardCandidate() returned nil")
	}
	if int(chosen.Product.ProductID) != 545918 {
		t.Fatalf("selectPreferredCardCandidate() product id = %d, want 545918", int(chosen.Product.ProductID))
	}
}

func TestSelectPreferredCardCandidateKeepsRequestedSpecialVariant(t *testing.T) {
	t.Parallel()

	candidates := []tcgplayer.MatchCandidate{
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   545918,
				ProductName: "Sabo",
				SetName:     "500 Years in the Future",
			},
			Score: 135,
		},
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   545919,
				ProductName: "Sabo (Parallel)",
				SetName:     "500 Years in the Future",
			},
			Score: 135,
		},
	}

	chosen := selectPreferredCardCandidate("OP07-118 500 Years in the Future parallel", candidates)
	if chosen == nil {
		t.Fatal("selectPreferredCardCandidate() returned nil")
	}
	if int(chosen.Product.ProductID) != 545919 {
		t.Fatalf("selectPreferredCardCandidate() product id = %d, want 545919", int(chosen.Product.ProductID))
	}
}

func TestSelectPreferredCardCandidatePrefersBaseOverAlternateArt(t *testing.T) {
	t.Parallel()

	candidates := []tcgplayer.MatchCandidate{
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   670254,
				ProductName: "Nami (Alternate Art)",
				SetName:     "The Azure Sea's Seven",
			},
			Score: 135,
		},
		{
			Product: tcgplayer.SearchProduct{
				ProductID:   670200,
				ProductName: "Nami",
				SetName:     "The Azure Sea's Seven",
			},
			Score: 135,
		},
	}

	chosen := selectPreferredCardCandidate("OP14-031 The Azure Sea's Seven", candidates)
	if chosen == nil {
		t.Fatal("selectPreferredCardCandidate() returned nil")
	}
	if int(chosen.Product.ProductID) != 670200 {
		t.Fatalf("selectPreferredCardCandidate() product id = %d, want 670200 (base Nami), got alternate art", int(chosen.Product.ProductID))
	}
}
