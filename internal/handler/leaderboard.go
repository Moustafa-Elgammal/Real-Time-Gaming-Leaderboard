package handler

import (
	"net/http"

	"example/real-time-gaming-leaderboard/internal/config"
	"example/real-time-gaming-leaderboard/internal/store"

	"github.com/gin-gonic/gin"
)

type Storer interface {
	TopN(n int) ([]store.UserRank, error)
	GetUserNeighborhood(username string) ([]store.UserRank, error)
	IncrementScore(username string) error
}

type Handler struct {
	store Storer
	topN  int
}

func New(s Storer, cfg *config.Config) *Handler {
	return &Handler{store: s, topN: cfg.TopN}
}

func (h *Handler) TopN(c *gin.Context) {
	ranks, err := h.store.TopN(h.topN)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, ranks)
}

func (h *Handler) GetUserRank(c *gin.Context) {
	username := c.Param("username")

	ranks, err := h.store.GetUserNeighborhood(username)
	if err != nil {
		c.IndentedJSON(http.StatusNotFound, nil)
		return
	}
	c.IndentedJSON(http.StatusOK, ranks)
}

func (h *Handler) UpdatePlayerScore(c *gin.Context) {
	username := c.Param("username")
	if username == "" {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"message": "username required"})
		return
	}

	if err := h.store.IncrementScore(username); err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, gin.H{"message": "saved"})
}
