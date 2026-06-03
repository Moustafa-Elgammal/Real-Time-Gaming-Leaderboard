package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"example/real-time-gaming-leaderboard/internal/config"
	"example/real-time-gaming-leaderboard/internal/store"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockStorer lets each test supply only the functions it needs.
type mockStorer struct {
	topNFunc               func(n int) ([]store.UserRank, error)
	getUserNeighborhoodFunc func(username string) ([]store.UserRank, error)
	incrementScoreFunc     func(username string) error
}

func (m *mockStorer) TopN(n int) ([]store.UserRank, error) {
	return m.topNFunc(n)
}

func (m *mockStorer) GetUserNeighborhood(username string) ([]store.UserRank, error) {
	return m.getUserNeighborhoodFunc(username)
}

func (m *mockStorer) IncrementScore(username string) error {
	return m.incrementScoreFunc(username)
}

func newTestHandler(s Storer, topN int) (*Handler, *gin.Engine) {
	cfg := &config.Config{TopN: topN}
	h := New(s, cfg)
	r := gin.New()
	r.GET("/v1/scores", h.TopN)
	r.GET("/v1/scores/:username", h.GetUserRank)
	r.POST("/v1/scores/:username", h.UpdatePlayerScore)
	return h, r
}

// --- TopN ---

func TestTopN_ReturnsRankedList(t *testing.T) {
	players := []store.UserRank{
		{Rank: 1, Username: "alice", Score: 900},
		{Rank: 2, Username: "bob", Score: 800},
	}
	mock := &mockStorer{
		topNFunc: func(n int) ([]store.UserRank, error) { return players, nil },
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var got []store.UserRank
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 || got[0].Username != "alice" {
		t.Errorf("unexpected body: %+v", got)
	}
}

func TestTopN_StoreError_Returns500(t *testing.T) {
	mock := &mockStorer{
		topNFunc: func(n int) ([]store.UserRank, error) {
			return nil, errors.New("redis down")
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

// --- GetUserRank ---

func TestGetUserRank_ReturnsNeighborhood(t *testing.T) {
	neighborhood := []store.UserRank{
		{Rank: 4, Username: "carol", Score: 750},
		{Rank: 5, Username: "alice", Score: 700},
		{Rank: 6, Username: "dave", Score: 650},
	}
	mock := &mockStorer{
		getUserNeighborhoodFunc: func(username string) ([]store.UserRank, error) {
			return neighborhood, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores/alice", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var got []store.UserRank
	json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 3 {
		t.Errorf("expected 3 entries, got %d", len(got))
	}
}

func TestGetUserRank_UnknownUser_Returns404(t *testing.T) {
	mock := &mockStorer{
		getUserNeighborhoodFunc: func(username string) ([]store.UserRank, error) {
			return nil, errors.New("redis: nil")
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores/ghost", nil))

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

// --- UpdatePlayerScore ---

func TestUpdatePlayerScore_Success_Returns200(t *testing.T) {
	mock := &mockStorer{
		incrementScoreFunc: func(username string) error { return nil },
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/scores/alice", nil))

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

func TestUpdatePlayerScore_StoreError_Returns500(t *testing.T) {
	mock := &mockStorer{
		incrementScoreFunc: func(username string) error {
			return errors.New("redis down")
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/scores/alice", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}
