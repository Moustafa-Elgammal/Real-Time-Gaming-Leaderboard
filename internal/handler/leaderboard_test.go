package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"moustafa-elgammal/real-time-gaming-leaderboard/internal/config"
	"moustafa-elgammal/real-time-gaming-leaderboard/internal/store"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// mockStorer lets each test supply only the functions it needs.
type mockStorer struct {
	topNPageFunc            func(offset, limit int) ([]store.UserRank, int64, error)
	getUserNeighborhoodFunc func(username string) ([]store.UserRank, error)
	incrementScoreFunc      func(username string) error
	subscribeFunc           func() (<-chan store.ScoreEvent, func(), error)
}

func (m *mockStorer) TopNPage(offset, limit int) ([]store.UserRank, int64, error) {
	return m.topNPageFunc(offset, limit)
}

func (m *mockStorer) GetUserNeighborhood(username string) ([]store.UserRank, error) {
	return m.getUserNeighborhoodFunc(username)
}

func (m *mockStorer) IncrementScore(username string) error {
	return m.incrementScoreFunc(username)
}

func (m *mockStorer) Subscribe() (<-chan store.ScoreEvent, func(), error) {
	return m.subscribeFunc()
}

func newTestHandler(s Storer, topN int) (*Handler, *gin.Engine) {
	cfg := &config.Config{TopN: topN}
	h := New(s, cfg)
	r := gin.New()
	r.GET("/v1/scores", h.TopN)
	r.GET("/v1/scores/stream", h.StreamScoreUpdates)
	r.GET("/v1/scores/:username", h.GetUserRank)
	r.POST("/v1/scores/:username", h.UpdatePlayerScore)
	return h, r
}

// --- TopN (paginated) ---

func TestTopN_DefaultPage_ReturnsPagedResponse(t *testing.T) {
	players := []store.UserRank{
		{Rank: 1, Username: "alice", Score: 900},
		{Rank: 2, Username: "bob", Score: 800},
	}
	mock := &mockStorer{
		topNPageFunc: func(offset, limit int) ([]store.UserRank, int64, error) {
			if offset != 0 || limit != 10 {
				t.Errorf("expected offset=0 limit=10, got offset=%d limit=%d", offset, limit)
			}
			return players, 2, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var got PagedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Data) != 2 || got.Data[0].Username != "alice" {
		t.Errorf("unexpected data: %+v", got.Data)
	}
	if got.Page != 1 || got.PageSize != 10 || got.Total != 2 {
		t.Errorf("unexpected pagination: page=%d page_size=%d total=%d", got.Page, got.PageSize, got.Total)
	}
}

func TestTopN_ExplicitPageAndPageSize(t *testing.T) {
	mock := &mockStorer{
		topNPageFunc: func(offset, limit int) ([]store.UserRank, int64, error) {
			if offset != 20 || limit != 10 {
				t.Errorf("expected offset=20 limit=10, got offset=%d limit=%d", offset, limit)
			}
			return []store.UserRank{{Rank: 21, Username: "carol", Score: 500}}, 50, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores?page=3&page_size=10", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var got PagedResponse
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Page != 3 || got.PageSize != 10 || got.Total != 50 {
		t.Errorf("unexpected pagination: %+v", got)
	}
}

func TestTopN_InvalidPage_Returns400(t *testing.T) {
	_, r := newTestHandler(&mockStorer{}, 10)

	for _, url := range []string{"/v1/scores?page=0", "/v1/scores?page=-1", "/v1/scores?page=abc"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", url, w.Code)
		}
	}
}

func TestTopN_InvalidPageSize_Returns400(t *testing.T) {
	_, r := newTestHandler(&mockStorer{}, 10)

	for _, url := range []string{"/v1/scores?page_size=0", "/v1/scores?page_size=101", "/v1/scores?page_size=abc"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", url, w.Code)
		}
	}
}

func TestTopN_StoreError_Returns500(t *testing.T) {
	mock := &mockStorer{
		topNPageFunc: func(offset, limit int) ([]store.UserRank, int64, error) {
			return nil, 0, errors.New("redis down")
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

func TestTopN_EmptyLeaderboard_ReturnsPageWithZeroTotal(t *testing.T) {
	mock := &mockStorer{
		topNPageFunc: func(offset, limit int) ([]store.UserRank, int64, error) {
			return []store.UserRank{}, 0, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var got PagedResponse
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Total != 0 || len(got.Data) != 0 {
		t.Errorf("expected empty page, got %+v", got)
	}
}

func TestTopN_PageSizeMaxBoundary_Returns200(t *testing.T) {
	mock := &mockStorer{
		topNPageFunc: func(offset, limit int) ([]store.UserRank, int64, error) {
			if limit != 100 {
				t.Errorf("expected limit=100, got %d", limit)
			}
			return []store.UserRank{}, 0, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores?page_size=100", nil))

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
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

// --- StreamScoreUpdates (SSE) ---

func TestStreamScoreUpdates_SendsEvent(t *testing.T) {
	// Buffered channel with one event; closing it terminates the stream.
	ch := make(chan store.ScoreEvent, 1)
	ch <- store.ScoreEvent{Username: "alice", Score: 10, Rank: 1}
	close(ch)

	mock := &mockStorer{
		subscribeFunc: func() (<-chan store.ScoreEvent, func(), error) {
			return ch, func() {}, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores/stream", nil))

	body := w.Body.String()
	if !strings.Contains(body, "score-update") {
		t.Errorf("expected SSE event name in body, got: %q", body)
	}
	if !strings.Contains(body, "alice") {
		t.Errorf("expected username in body, got: %q", body)
	}
}

func TestStreamScoreUpdates_MultipleEvents(t *testing.T) {
	ch := make(chan store.ScoreEvent, 3)
	ch <- store.ScoreEvent{Username: "alice", Score: 10, Rank: 1}
	ch <- store.ScoreEvent{Username: "bob", Score: 9, Rank: 2}
	ch <- store.ScoreEvent{Username: "carol", Score: 8, Rank: 3}
	close(ch)

	mock := &mockStorer{
		subscribeFunc: func() (<-chan store.ScoreEvent, func(), error) {
			return ch, func() {}, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores/stream", nil))

	body := w.Body.String()
	for _, name := range []string{"alice", "bob", "carol"} {
		if !strings.Contains(body, name) {
			t.Errorf("expected %q in SSE body, got: %q", name, body)
		}
	}
}

func TestStreamScoreUpdates_SubscribeError_Returns500(t *testing.T) {
	mock := &mockStorer{
		subscribeFunc: func() (<-chan store.ScoreEvent, func(), error) {
			return nil, nil, errors.New("redis unavailable")
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores/stream", nil))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

func TestStreamScoreUpdates_UnsubscribeCalledOnExit(t *testing.T) {
	ch := make(chan store.ScoreEvent)
	close(ch)

	unsubscribeCalled := false
	mock := &mockStorer{
		subscribeFunc: func() (<-chan store.ScoreEvent, func(), error) {
			return ch, func() { unsubscribeCalled = true }, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/scores/stream", nil))

	if !unsubscribeCalled {
		t.Error("expected unsubscribe to be called when stream exits")
	}
}

func TestStreamScoreUpdates_ContentTypeIsEventStream(t *testing.T) {
	ch := make(chan store.ScoreEvent)
	close(ch)

	mock := &mockStorer{
		subscribeFunc: func() (<-chan store.ScoreEvent, func(), error) {
			return ch, func() {}, nil
		},
	}
	_, r := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/scores/stream", nil))

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}
}

// Gin's router requires a non-empty path segment for :username so the empty-
// string guard in UpdatePlayerScore is never reached via normal routing.
// This test calls the handler directly to verify the defensive check works.
func TestUpdatePlayerScore_EmptyUsername_Returns400(t *testing.T) {
	mock := &mockStorer{}
	h, _ := newTestHandler(mock, 10)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "username", Value: ""}}

	h.UpdatePlayerScore(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}
