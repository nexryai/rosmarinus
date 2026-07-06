package main

import (
	"context"
	"log"
	"os"

	"github.com/nexryai/rosmarinus/internal/app"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := app.Run(context.Background(), logger); err != nil {
		logger.Printf("rosmarinus stopped: %v", err)
		os.Exit(1)
	}
}
