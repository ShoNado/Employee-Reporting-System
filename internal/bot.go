package internal

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"telegram-bot/config"
	"telegram-bot/db"
	"telegram-bot/models"
	"time"

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

	// Handle file uploads
	if message.Document != nil {
		b.handleFileUpload(message)
		return
	}

	// Handle commands
	if message.IsCommand() {
		b.handleCommand(message)
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

// handleFileUpload handles file uploads
func (b *Bot) handleFileUpload(message *tgbotapi.Message) {
	file := message.Document
	fileID := file.FileID

	// Get file from Telegram
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	fileData, err := b.API.GetFile(fileConfig)
	if err != nil {
		log.Printf("Error getting file: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при получении файла.")
		b.API.Send(msg)
		return
	}

	// Download file
	fileURL := fileData.Link(b.API.Token)
	resp, err := http.Get(fileURL)
	if err != nil {
		log.Printf("Error downloading file: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при скачивании файла.")
		b.API.Send(msg)
		return
	}
	defer resp.Body.Close()

	// Read file data
	fileBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading file: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при чтении файла.")
		b.API.Send(msg)
		return
	}

	// Create file record
	dbFile := &models.File{
		UserID:    message.From.ID,
		FileName:  file.FileName,
		FileType:  file.MimeType,
		FileData:  fileBytes,
		CreatedAt: time.Now(),
	}

	// Save file to database
	err = b.DB.SaveFile(dbFile)
	if err != nil {
		if err.Error() == "file already exists" {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Этот файл уже был загружен ранее.")
			b.API.Send(msg)
			return
		}
		log.Printf("Error saving file: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при сохранении файла.")
		b.API.Send(msg)
		return
	}

	// Send confirmation with file ID
	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Файл успешно сохранен.\nID файла: %d\nИмя файла: %s\nТип файла: %s",
		dbFile.ID, dbFile.FileName, dbFile.FileType))
	b.API.Send(msg)
}

// handleCommand handles bot commands
func (b *Bot) handleCommand(message *tgbotapi.Message) {
	command := message.Command()
	args := message.CommandArguments()

	switch command {
	case "show":
		if args == "" {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Пожалуйста, укажите ID файла. Например: /show 1")
			b.API.Send(msg)
			return
		}

		var fileID int64
		_, err := fmt.Sscanf(args, "%d", &fileID)
		if err != nil {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Неверный формат ID файла.")
			b.API.Send(msg)
			return
		}

		// Get file from database
		file, err := b.DB.GetFile(fileID)
		if err != nil {
			log.Printf("Error getting file: %v", err)
			msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при получении файла.")
			b.API.Send(msg)
			return
		}

		if file == nil {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Файл не найден.")
			b.API.Send(msg)
			return
		}

		// Check permissions
		user, err := b.DB.GetUser(message.From.ID)
		if err != nil {
			log.Printf("Error getting user: %v", err)
			msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при проверке прав доступа.")
			b.API.Send(msg)
			return
		}

		if !user.IsAdmin && file.UserID != message.From.ID {
			msg := tgbotapi.NewMessage(message.Chat.ID, "У вас нет прав для доступа к этому файлу.")
			b.API.Send(msg)
			return
		}

		// Send file back to user
		fileBytes := tgbotapi.FileBytes{
			Name:  file.FileName,
			Bytes: file.FileData,
		}

		doc := tgbotapi.NewDocument(message.Chat.ID, fileBytes)
		b.API.Send(doc)
	}
}
