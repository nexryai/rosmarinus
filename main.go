package main

import (
	"context"
	"log"
	"os"

	"github.com/nexryai/rosmarinus/internal/app"
	"github.com/nexryai/rosmarinus/internal/config"
	"github.com/nexryai/rosmarinus/internal/queue"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if len(os.Args) > 1 && os.Args[1] == "queue" {
		cfg, err := config.LoadFromEnv()
		if err == nil {
			err = queue.RunOperationsCLI(context.Background(), os.Args[2:], queue.RedisConfig{
				Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB,
			}, os.Stdout)
		}
		if err != nil {
			logger.Printf("queue operation failed: %v", err)
			os.Exit(1)
		}
		return
	}
	if err := app.Run(context.Background(), logger); err != nil {
		logger.Printf("rosmarinus stopped: %v", err)
		os.Exit(1)
	}
}
