package mercariwatch

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

func LoadDotEnv(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexRune(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func EnvString(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return fallback
}

func EnvBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(EnvString(key, "")))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func EnvInt(key string, fallback int) int {
	value := strings.TrimSpace(EnvString(key, ""))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func EnvFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(EnvString(key, ""))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func EnvCSV(key string, fallback []string) []string {
	value := strings.TrimSpace(EnvString(key, ""))
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func Seconds(n int) time.Duration {
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
