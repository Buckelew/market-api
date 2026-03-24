module market-api

go 1.25.7

require (
	discogs v0.0.0-00010101000000-000000000000
	github.com/Buckelew/card v0.0.0-20260306045539-943eae736bc7
	github.com/google/uuid v1.6.0
	modernc.org/sqlite v1.39.1
)

replace discogs => ../cli/discogs

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp v0.0.0-20250620022241-b7579e27df2b // indirect
	golang.org/x/sys v0.36.0 // indirect
	modernc.org/libc v1.66.10 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
