package internal

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
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

// verifyAdmins checks and updates admin status for all users in the database
func (b *Bot) verifyAdmins() error {
	// Update all admin statuses at once using the config
	return b.DB.UpdateAdminStatuses(b.Config.Admins)
}

// NewBot creates a new Bot instance
func NewBot(botToken string, database *db.DB, cfg *config.Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bot: %w", err)
	}

	bot := &Bot{
		API:    api,
		DB:     database,
		Config: cfg,
	}

	// Verify admin statuses at startup
	if err := bot.verifyAdmins(); err != nil {
		return nil, fmt.Errorf("failed to verify admins: %w", err)
	}

	return bot, nil
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

			// Remove keyboard and send confirmation
			msg := tgbotapi.NewMessage(message.Chat.ID, "Спасибо! Ваш номер телефона сохранен.")
			msg.ReplyMarkup = tgbotapi.NewRemoveKeyboard(true)
			b.API.Send(msg)
			return
		}

		// If not a contact, request phone number again
		b.requestPhoneNumber(message.Chat.ID)
		return
	}

	// Handle different types of media
	if message.Document != nil {
		b.handleFileUpload(message, message.Document.FileID, message.Document.FileName, message.Document.MimeType)
		return
	}

	if message.Photo != nil {
		// Get the largest photo size
		photo := message.Photo[len(message.Photo)-1]
		// Check if it's a HEIC image
		if strings.HasSuffix(strings.ToLower(message.Caption), ".heic") {
			b.handleFileUpload(message, photo.FileID, "photo.heic", "image/heic")
		} else {
			b.handleFileUpload(message, photo.FileID, "photo.jpg", "image/jpeg")
		}
		return
	}

	if message.Voice != nil {
		// Handle voice message
		b.handleFileUpload(message, message.Voice.FileID, "voice.ogg", "audio/ogg")
		return
	}

	if message.Audio != nil {
		// Handle audio file
		fileName := message.Audio.FileName
		if fileName == "" {
			fileName = "audio.mp3"
		}
		b.handleFileUpload(message, message.Audio.FileID, fileName, "audio/mpeg")
		return
	}

	if message.Video != nil {
		// Handle video file
		fileName := message.Video.FileName
		if fileName == "" {
			// Check if it's a MOV file
			if strings.HasSuffix(strings.ToLower(message.Caption), ".mov") {
				fileName = "video.mov"
			} else {
				fileName = "video.mp4"
			}
		}
		b.handleFileUpload(message, message.Video.FileID, fileName, "video/mp4")
		return
	}

	if message.VideoNote != nil {
		// Handle video note (circular video)
		b.handleFileUpload(message, message.VideoNote.FileID, "video_note.mp4", "video/mp4")
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

	// Handle delete confirmation
	if strings.HasPrefix(callback.Data, "confirm_delete_") {
		fileID, _ := strconv.ParseInt(strings.TrimPrefix(callback.Data, "confirm_delete_"), 10, 64)

		// Delete file
		err := b.DB.DeleteFile(fileID)
		if err != nil {
			log.Printf("Error deleting file: %v", err)
			msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "Ошибка при удалении файла.")
			b.API.Send(msg)
			return
		}

		msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "Файл успешно удален.")
		b.API.Send(msg)
	} else if strings.HasPrefix(callback.Data, "cancel_delete_") {
		msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "Удаление файла отменено.")
		b.API.Send(msg)
	}

	// Respond to callback
	b.API.Request(tgbotapi.NewCallback(callback.ID, ""))
}

// handleFileUpload handles file uploads
func (b *Bot) handleFileUpload(message *tgbotapi.Message, fileID, fileName, fileType string) {
	// Get file from Telegram
	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	fileData, err := b.API.GetFile(fileConfig)
	if err != nil {
		// Check if error is due to file size
		if strings.Contains(err.Error(), "file is too big") {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Извините, файл слишком большой. Максимальный размер файла - 100 МБ.")
			b.API.Send(msg)
			return
		}
		log.Printf("Error getting file: %v", err)
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при получении файла.")
		b.API.Send(msg)
		return
	}

	// Check file size (100MB limit)
	const maxFileSize = 100 * 1024 * 1024 // 100MB in bytes
	if fileData.FileSize > maxFileSize {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Извините, файл слишком большой. Максимальный размер файла - 100 МБ.")
		b.API.Send(msg)
		return
	}

	// Send loading message
	loadingMsg := tgbotapi.NewMessage(message.Chat.ID, "⏳ Файл загружается в базу данных, пожалуйста, подождите...")
	b.API.Send(loadingMsg)

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
		FileName:  fileName,
		FileType:  fileType,
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
	msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("✅ Файл успешно сохранен.\nID файла: %d\nИмя файла: %s\nТип файла: %s",
		dbFile.ID, dbFile.FileName, dbFile.FileType))
	b.API.Send(msg)
}

// handleCommand handles bot commands
func (b *Bot) handleCommand(message *tgbotapi.Message) {
	command := message.Command()
	args := message.CommandArguments()

	switch command {
	case "start":
		helpText := `Добро пожаловать! Я бот для хранения и управления файлами.

Доступные команды:
/list - показать список ваших файлов
/show <id> - показать файл по его ID
/delete <id> - удалить файл по его ID

Ограничения:
- Максимальный размер файла: 100 МБ
- Поддерживаемые типы файлов: документы, фото, видео, голосовые сообщения

Для начала работы просто отправьте мне любой файл!`

		msg := tgbotapi.NewMessage(message.Chat.ID, helpText)
		b.API.Send(msg)

	case "list":
		// Get user's files
		files, err := b.DB.GetUserFiles(message.From.ID)
		if err != nil {
			log.Printf("Error getting user files: %v", err)
			msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при получении списка файлов.")
			b.API.Send(msg)
			return
		}

		// If user is admin, get all files
		user, err := b.DB.GetUser(message.From.ID)
		if err != nil {
			log.Printf("Error getting user: %v", err)
			msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при проверке прав доступа.")
			b.API.Send(msg)
			return
		}

		if user.IsAdmin {
			// Get all files from all users
			allFiles, err := b.DB.GetAllFiles()
			if err != nil {
				log.Printf("Error getting all files: %v", err)
				msg := tgbotapi.NewMessage(message.Chat.ID, "Ошибка при получении списка файлов.")
				b.API.Send(msg)
				return
			}
			files = allFiles
		}

		if len(files) == 0 {
			msg := tgbotapi.NewMessage(message.Chat.ID, "У вас нет доступных файлов.")
			b.API.Send(msg)
			return
		}

		// Format file list
		var response strings.Builder
		for i, file := range files {
			response.WriteString(fmt.Sprintf("%d. %s (ID: %d)\n", i+1, file.FileName, file.ID))
		}

		msg := tgbotapi.NewMessage(message.Chat.ID, response.String())
		b.API.Send(msg)

	case "show":
		if args == "" {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Пожалуйста, укажите ID файла. Например: /show 1")
			b.API.Send(msg)
			return
		}

		// Send immediate response
		msg := tgbotapi.NewMessage(message.Chat.ID, "Ожидайте загрузки файла...")
		b.API.Send(msg)

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

	case "delete":
		if args == "" {
			msg := tgbotapi.NewMessage(message.Chat.ID, "Пожалуйста, укажите ID файла. Например: /delete 1")
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
			msg := tgbotapi.NewMessage(message.Chat.ID, "У вас нет прав для удаления этого файла.")
			b.API.Send(msg)
			return
		}

		// Create inline keyboard for confirmation
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("✅ Подтвердить", fmt.Sprintf("confirm_delete_%d", fileID)),
				tgbotapi.NewInlineKeyboardButtonData("❌ Отмена", fmt.Sprintf("cancel_delete_%d", fileID)),
			),
		)

		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Вы уверены, что хотите удалить файл %s?", file.FileName))
		msg.ReplyMarkup = keyboard
		b.API.Send(msg)

	default:
		msg := tgbotapi.NewMessage(message.Chat.ID, "Извините, такой команды не существует. Используйте /start для получения списка доступных команд.")
		b.API.Send(msg)
	}
}
