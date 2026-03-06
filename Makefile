.PHONY: run test build tidy generate-image-testdata

run:
	go run ./cmd/server

test:
	go test ./...

build:
	go build ./...

tidy:
	go mod tidy

generate-image-testdata:
	go run ./cmd/generate-image-testdata -out testdata/one_piece_image_cases.json -limit 8 -max-cases 12
