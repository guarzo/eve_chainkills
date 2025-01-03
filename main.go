package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

func main() {
	// 1) Load configuration
	cfg, err := LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// 2) Create logger (simple version using stdlib)
	logger := logrus.New()

	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors:   true,
		FullTimestamp: false,
	})

	lvl, parseErr := logrus.ParseLevel(strings.ToLower(cfg.LogLevel))
	if parseErr != nil {
		logger.Warnf("Invalid log level '%s', defaulting to 'info'", cfg.LogLevel)
		lvl = logrus.InfoLevel
	}
	logger.SetLevel(lvl)

	// 3) Initialize and start the ChainKillChecker
	ckChecker, err := NewChainKillChecker(logger, cfg)
	if err != nil {
		logger.Fatalf("Failed to create ChainKillChecker: %v\n", err)
	}

	// Start the zKillboard WebSocket listener
	go ckChecker.StartListening()

	logger.Println("Started chain kill checker.")

	// 5) Listen for OS signals so we can gracefully shut down
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigs
	logger.Printf("Received signal: %s, shutting down.", sig)
	ckChecker.Close()
	time.Sleep(1 * time.Second)
}
