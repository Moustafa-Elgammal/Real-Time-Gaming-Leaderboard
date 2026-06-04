package handler

import (
	"net/http"
	"strconv"

	"example/real-time-gaming-leaderboard/internal/config"
	"example/real-time-gaming-leaderboard/internal/store"

	"github.com/gin-gonic/gin"
)

const maxPageSize = 100

type Storer interface {
	TopNPage(offset, limit int) ([]store.UserRank, int64, error)
	GetUserNeighborhood(username string) ([]store.UserRank, error)
	IncrementScore(username string) error
}

// PagedResponse wraps a leaderboard page with pagination metadata.
type PagedResponse struct {
	Data     []store.UserRank `json:"data"`
	Page     int              `json:"page"      example:"1"`
	PageSize int              `json:"page_size" example:"10"`
	Total    int64            `json:"total"     example:"150"`
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
//	@Summary      Get top scores (paginated)
//	@Description  Returns a page of players for the current calendar month, ordered by score descending. Defaults to page 1 with page_size equal to the TOP_N configuration value.
//	@Tags         leaderboard
//	@Produce      json
//	@Security     InternalToken
//	@Param        page       query     int  false  "Page number (1-based)"        default(1)
//	@Param        page_size  query     int  false  "Number of entries per page (1-100)"  default(10)
//	@Success      200  {object}  handler.PagedResponse
//	@Failure      400  {object}  map[string]string  "Invalid page or page_size"
//	@Failure      500  {object}  map[string]string
//	@Router       /v1/scores [get]
func (h *Handler) TopN(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "page must be a positive integer"})
		return
	}

	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(h.topN)))
	if err != nil || pageSize < 1 || pageSize > maxPageSize {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "page_size must be between 1 and 100"})
		return
	}

	offset := (page - 1) * pageSize
	ranks, total, err := h.store.TopNPage(offset, pageSize)
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.IndentedJSON(http.StatusOK, PagedResponse{
		Data:     ranks,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	})
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
