// @title           Real-Time Gaming Leaderboard API
// @version         1.0
// @description     Internal leaderboard service. Designed to sit behind a game service that handles player authentication, session management, anti-cheat, and per-player rate limiting. All requests must include the X-Internal-Token header when INTERNAL_API_KEY is configured.
// @contact.name    Moustafa Elgammal
// @contact.email   moustafa_algammal@yahoo.com
// @host            localhost:8080
// @BasePath        /
// @securityDefinitions.apikey  InternalToken
// @in              header
// @name            X-Internal-Token
// @description     Shared secret set via INTERNAL_API_KEY env var. Leave empty to disable (local dev).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "moustafa-elgammal/real-time-gaming-leaderboard/docs"
	"moustafa-elgammal/real-time-gaming-leaderboard/internal/config"
	"moustafa-elgammal/real-time-gaming-leaderboard/internal/handler"
	"moustafa-elgammal/real-time-gaming-leaderboard/internal/middleware"
	"moustafa-elgammal/real-time-gaming-leaderboard/internal/service"
	"moustafa-elgammal/real-time-gaming-leaderboard/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
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
	router.Use(middleware.InternalAuth(cfg.InternalAPIKey))
	router.GET("/v1/scores", h.TopN)
	router.GET("/v1/scores/stream", h.StreamScoreUpdates)
	router.GET("/v1/scores/:username", h.GetUserRank)
	router.POST("/v1/scores/:username", h.UpdatePlayerScore)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

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
