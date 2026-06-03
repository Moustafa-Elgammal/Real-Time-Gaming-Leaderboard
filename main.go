package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example/real-time-gaming-leaderboard/internal/config"
	"example/real-time-gaming-leaderboard/internal/handler"
	"example/real-time-gaming-leaderboard/internal/service"
	"example/real-time-gaming-leaderboard/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using environment variables")
	}

	cfg := config.Load()

	mysql, err := store.NewMySQL(cfg)
	if err != nil {
		log.Fatalf("mysql: %v", err)
	}

	batcher := store.NewEventBatcher(mysql, cfg)
	redis := store.New(cfg)
	svc := service.New(batcher, redis)

	if err := svc.RecoverCurrentMonth(); err != nil {
		log.Fatalf("leaderboard recovery: %v", err)
	}

	h := handler.New(svc, cfg)

	router := gin.Default()
	router.GET("/v1/scores", h.TopN)
	router.GET("/v1/scores/:username", h.GetUserRank)
	router.POST("/v1/scores/:username", h.UpdatePlayerScore)

	srv := &http.Server{Addr: ":8080", Handler: router}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down — flushing event buffer...")
	batcher.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}
