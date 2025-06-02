package internal

import (
	"fmt"
	"log"
	"telegram-bot/config"
	"telegram-bot/db"
	"telegram-bot/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot represents the Telegram bot and its dependencies
type Bot struct {
	API    *tgbotapi.BotAPI
	DB     *db.DB
	Config *config.Config
}

// NewBot creates a new Bot instance
func NewBot(botToken string, database *db.DB, cfg *config.Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bot: %w", err)
	}

	return &Bot{
		API:    api,
		DB:     database,
		Config: cfg,
	}, nil
}

// Start starts the bot and listens for updates
func (b *Bot) Start() error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.API.GetUpdatesChan(u)

	for update := range updates {
		// Handle different types of updates
		if update.Message != nil {
			go b.handleMessage(update.Message)
		} else if update.CallbackQuery != nil {
			go b.handleCallback(update.CallbackQuery)
		}
	}

	return nil
}

// handleMessage handles incoming messages
func (b *Bot) handleMessage(message *tgbotapi.Message) {
	// Log message to console
	log.Printf("[%s] %s", message.From.UserName, message.Text)

	// Save user info to database
	user := &models.User{
		ID:        message.From.ID,
		Username:  message.From.UserName,
		FirstName: message.From.FirstName,
		LastName:  message.From.LastName,
		IsAdmin:   b.Config.IsAdmin(message.From.UserName),
	}

	// Check if user exists in the database
	existingUser, err := b.DB.GetUser(user.ID)
	if err != nil {
		log.Printf("Error getting user: %v", err)
	}

	// If user does not exist, save to DB and request phone number
	if existingUser == nil {
		if err := b.DB.SaveUser(user); err != nil {
			log.Printf("Error saving user: %v", err)
		}
		b.requestPhoneNumber(message.Chat.ID)
		return
	}

	// If user exists but doesn't have a phone number yet
	if existingUser.Phone == "" {
		// If this message contains a contact, save the phone number
		if message.Contact != nil {
			phone := message.Contact.PhoneNumber
			if err := b.DB.UpdateUserPhone(user.ID, phone); err != nil {
				log.Printf("Error updating phone: %v", err)
			}

			msg := tgbotapi.NewMessage(message.Chat.ID, "Спасибо! Ваш номер телефона сохранен.")
			b.API.Send(msg)
			return
		}

		// If not a contact, request phone number again
		b.requestPhoneNumber(message.Chat.ID)
		return
	}

	// Process regular messages
	b.processMessage(message)
}

// requestPhoneNumber asks the user for their phone number
func (b *Bot) requestPhoneNumber(chatID int64) {
	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButtonContact("Поделиться номером телефона"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, "Пожалуйста, поделитесь своим номером телефона.")
	msg.ReplyMarkup = keyboard

	b.API.Send(msg)
}

// processMessage processes regular messages
func (b *Bot) processMessage(message *tgbotapi.Message) {
	// Get user status
	user, err := b.DB.GetUser(message.From.ID)
	if err != nil {
		log.Printf("Error getting user: %v", err)
		return
	}

	// Check if admin
	statusText := "обычный пользователь"
	if user.IsAdmin {
		statusText = "администратор"
	}

	responseText := fmt.Sprintf("Ваше сообщение получено, %s.\nВаш статус: %s", user.FirstName, statusText)

	msg := tgbotapi.NewMessage(message.Chat.ID, responseText)
	b.API.Send(msg)
}

// handleCallback handles callback queries from inline keyboards
func (b *Bot) handleCallback(callback *tgbotapi.CallbackQuery) {
	// Handle any callback queries here
	log.Printf("[CALLBACK] %s: %s", callback.From.UserName, callback.Data)

	// Respond to callback
	b.API.Request(tgbotapi.NewCallback(callback.ID, ""))
}
