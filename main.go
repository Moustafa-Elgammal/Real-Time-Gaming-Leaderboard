package main

import (
	"log"

	"example/real-time-gaming-leaderboard/internal/config"
	"example/real-time-gaming-leaderboard/internal/handler"
	"example/real-time-gaming-leaderboard/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using environment variables")
	}

	cfg := config.Load()
	s := store.New(cfg)
	h := handler.New(s, cfg)

	router := gin.Default()
	router.GET("/v1/scores", h.TopN)
	router.GET("/v1/scores/:username", h.GetUserRank)
	router.POST("/v1/scores/:username", h.UpdatePlayerScore)
	router.Run(":8080")
}
