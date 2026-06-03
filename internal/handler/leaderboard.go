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

// TopN godoc
//
//	@Summary      Get top scores
//	@Description  Returns the top TOP_N players for the current calendar month, ordered by score descending.
//	@Tags         leaderboard
//	@Produce      json
//	@Security     InternalToken
//	@Success      200  {array}   store.UserRank
//	@Failure      500  {object}  map[string]string
//	@Router       /v1/scores [get]
func (h *Handler) TopN(c *gin.Context) {
	ranks, err := h.store.TopN(h.topN)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, ranks)
}

// GetUserRank godoc
//
//	@Summary      Get player rank and neighbors
//	@Description  Returns the player along with up to USER_NEIGHBORHOOD players ranked immediately above and below them in the current month's leaderboard.
//	@Tags         leaderboard
//	@Produce      json
//	@Security     InternalToken
//	@Param        username  path      string  true  "Player username"
//	@Success      200       {array}   store.UserRank
//	@Failure      404       {object}  map[string]string  "Player not on leaderboard"
//	@Router       /v1/scores/{username} [get]
func (h *Handler) GetUserRank(c *gin.Context) {
	username := c.Param("username")

	ranks, err := h.store.GetUserNeighborhood(username)
	if err != nil {
		c.IndentedJSON(http.StatusNotFound, nil)
		return
	}
	c.IndentedJSON(http.StatusOK, ranks)
}

// UpdatePlayerScore godoc
//
//	@Summary      Increment player score
//	@Description  Adds 1 point to the player's score for the current month. Called by the game service after it has authenticated the player and validated the score event. Creates the player entry if it does not exist.
//	@Tags         leaderboard
//	@Produce      json
//	@Security     InternalToken
//	@Param        username  path      string  true  "Player username"
//	@Success      200       {object}  map[string]string
//	@Failure      400       {object}  map[string]string  "Missing username"
//	@Failure      500       {object}  map[string]string  "MySQL or Redis write failure"
//	@Router       /v1/scores/{username} [post]
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
