package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"telegram-bot/config"
	"telegram-bot/db"
	"telegram-bot/internal"
)

func main() {
	log.Println("Starting Telegram bot...")

	// Determine executable directory to find config and DB relative to it
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	baseDir := filepath.Dir(exePath)

	// Define paths relative to executable
	configPath := filepath.Join(baseDir, "config", "config.json")
	dbPath := filepath.Join(baseDir, "db", "telegram.db")

	// Try current directory if config doesn't exist at executable path
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		currentDir, _ := os.Getwd()
		configPath = filepath.Join(currentDir, "config", "config.json")
		dbPath = filepath.Join(currentDir, "db", "telegram.db")
	}

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Check if bot token is set
	if cfg.BotToken == "YOUR_BOT_TOKEN_HERE" {
		log.Fatalf("Please set your bot token in %s", configPath)
	}

	// Check if MongoDB URI is set
	if cfg.MongoURI == "" {
		log.Fatalf("Please set MongoDB URI in %s", configPath)
	}

	// Ensure DB directory exists
	dbDir := filepath.Dir(dbPath)
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dbDir, os.ModePerm); err != nil {
			log.Fatalf("Failed to create database directory: %v", err)
		}
	}

	// Initialize database
	database, err := db.NewDB(dbPath, cfg.MongoURI)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	if err := database.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Initialize and start the bot
	bot, err := internal.NewBot(cfg.BotToken, database, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}

	log.Println("Bot started successfully!")

	// Start the bot in a goroutine
	go func() {
		if err := bot.Start(); err != nil {
			log.Fatalf("Error running bot: %v", err)
		}
	}()

	// Wait for termination signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Bot stopping...")
}
