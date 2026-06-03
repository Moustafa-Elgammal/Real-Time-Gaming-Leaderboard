package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newRouter(key string) *gin.Engine {
	r := gin.New()
	r.Use(InternalAuth(key))
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestInternalAuth_Disabled_WhenKeyEmpty(t *testing.T) {
	r := newRouter("")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when auth disabled, got %d", w.Code)
	}
}

func TestInternalAuth_Allows_CorrectToken(t *testing.T) {
	r := newRouter("secret")

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(internalTokenHeader, "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", w.Code)
	}
}

func TestInternalAuth_Rejects_WrongToken(t *testing.T) {
	r := newRouter("secret")

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(internalTokenHeader, "wrong")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong token, got %d", w.Code)
	}
}

func TestInternalAuth_Rejects_MissingToken(t *testing.T) {
	r := newRouter("secret")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with missing token, got %d", w.Code)
	}
}
