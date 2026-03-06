package main

import "testing"

func TestRedactCardCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		cardCode string
		want     string
	}{
		{
			name:     "removes spaced starter code",
			input:    "2025 One Piece CCG English ST 21-001 Monkey D Luffy",
			cardCode: "ST21-001",
			want:     "2025 One Piece CCG English Monkey D Luffy",
		},
		{
			name:     "removes exact hyphenated code",
			input:    "Boa Hancock OP12-014 SR Legacy of the Master",
			cardCode: "OP12-014",
			want:     "Boa Hancock SR Legacy of the Master",
		},
		{
			name:     "removes promo shorthand",
			input:    "Monkey D. Luffy (JP) P-043",
			cardCode: "P-043",
			want:     "Monkey D. Luffy JP",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := redactCardCode(tt.input, tt.cardCode)
			if got != tt.want {
				t.Fatalf("redactCardCode(%q, %q) = %q, want %q", tt.input, tt.cardCode, got, tt.want)
			}
		})
	}
}

func TestInferLanguage(t *testing.T) {
	t.Parallel()

	if got := inferLanguage("Monkey D. Luffy (JP)", "", "japanese one piece card lot"); got != "jp" {
		t.Fatalf("inferLanguage jp = %q, want jp", got)
	}
	if got := inferLanguage("Boa Hancock", "English Edition", "one piece op"); got != "en" {
		t.Fatalf("inferLanguage en = %q, want en", got)
	}
}
