package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"market-api/internal/db"
	"market-api/internal/models"
	"market-api/internal/resolve"
)

type Server struct {
	store            *db.Store
	resolver         *resolve.Service
	batchConcurrency int
	authToken        string
}

func NewServer(store *db.Store, resolver *resolve.Service, batchConcurrency int, authToken string) *Server {
	return &Server{
		store:            store,
		resolver:         resolver,
		batchConcurrency: batchConcurrency,
		authToken:        strings.TrimSpace(authToken),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/resolve", s.handleResolve)
	mux.HandleFunc("POST /v1/resolve/batch", s.handleResolveBatch)
	mux.HandleFunc("GET /v1/requests/{id}", s.handleGetRequest)
	mux.HandleFunc("GET /v1/results/{id}", s.handleGetResult)
	return s.authMiddleware(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.store.Health(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, models.ErrorResponse{Error: "database unavailable"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeResolveRequest(w, r)
	if !ok {
		return
	}

	response, err := s.resolver.Resolve(r.Context(), input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "resolve failed"})
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleResolveBatch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var input models.BatchResolveRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid batch request"})
		return
	}

	if len(input.Requests) == 0 {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "requests must not be empty"})
		return
	}

	type jobResult struct {
		index  int
		result models.BatchItemResult
	}

	jobs := make(chan int)
	results := make(chan jobResult, len(input.Requests))

	workerCount := s.batchConcurrency
	if workerCount > len(input.Requests) {
		workerCount = len(input.Requests)
	}

	for range workerCount {
		go func() {
			for index := range jobs {
				request := input.Requests[index]
				if err := validateInput(request); err != nil {
					results <- jobResult{
						index: index,
						result: models.BatchItemResult{
							Index: index,
							Error: err.Error(),
						},
					}
					continue
				}

				response, err := s.resolver.Resolve(r.Context(), request)
				item := models.BatchItemResult{Index: index}
				if err != nil {
					item.Error = "resolve failed"
				} else {
					item.Result = response
				}
				results <- jobResult{index: index, result: item}
			}
		}()
	}

	for index := range input.Requests {
		jobs <- index
	}
	close(jobs)

	ordered := make([]models.BatchItemResult, len(input.Requests))
	for range input.Requests {
		result := <-results
		ordered[result.index] = result.result
	}

	writeJSON(w, http.StatusOK, models.BatchResolveResponse{Results: ordered})
}

func (s *Server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	record, err := s.store.GetRequest(r.Context(), r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, models.ErrorResponse{Error: "request not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "lookup failed"})
		return
	}

	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleGetResult(w http.ResponseWriter, r *http.Request) {
	record, err := s.store.GetResult(r.Context(), r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, models.ErrorResponse{Error: "result not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "lookup failed"})
		return
	}

	writeJSON(w, http.StatusOK, record)
}

func decodeResolveRequest(w http.ResponseWriter, r *http.Request) (models.ResolveRequestInput, bool) {
	defer r.Body.Close()

	var input models.ResolveRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body"})
		return models.ResolveRequestInput{}, false
	}

	if err := validateInput(input); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return models.ResolveRequestInput{}, false
	}

	return input, true
}

func validateInput(input models.ResolveRequestInput) error {
	if strings.TrimSpace(input.Title) == "" && strings.TrimSpace(input.Description) == "" && strings.TrimSpace(input.ImageURL) == "" && len(input.ImageURLs) == 0 {
		return errors.New("at least one of title, description, image_url, or image_urls is required")
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	if s.authToken == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		if authorized(r, s.authToken) {
			next.ServeHTTP(w, r)
			return
		}

		writeJSON(w, http.StatusUnauthorized, models.ErrorResponse{Error: "unauthorized"})
	})
}

func authorized(r *http.Request, token string) bool {
	if compareToken(r.Header.Get("X-API-Key"), token) {
		return true
	}

	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return compareToken(strings.TrimSpace(authHeader[7:]), token)
	}

	return false
}

func compareToken(got, want string) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
