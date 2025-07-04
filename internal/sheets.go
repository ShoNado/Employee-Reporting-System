package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"telegram-bot/models"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const (
	// ID таблицы Google Sheets
	spreadsheetID = "13KIfRMTePI4djpi6W4pm6WKaE0I9sTA4-LPkesS724I"

	// Диапазон для записи данных (лист и диапазон)
	readRange = "Лист1!A1:Z1000"

	// Статусы файлов
	FileStatusActive  = "Активен"
	FileStatusDeleted = "Удален"
)

// SheetsService представляет сервис для работы с Google Sheets
type SheetsService struct {
	service *sheets.Service
}

// NewSheetsService создает новый сервис для работы с Google Sheets
func NewSheetsService(credentialsFile string) (*SheetsService, error) {
	// Чтение файла с учетными данными
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file: %v", err)
	}

	// Создание конфигурации из учетных данных
	// Если учетные данные в JSON формате
	config, err := google.ConfigFromJSON(b, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse client secret file to config: %v", err)
	}

	// Получение токена
	client := getClient(config)

	// Создание сервиса Google Sheets
	srv, err := sheets.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Sheets client: %v", err)
	}

	return &SheetsService{service: srv}, nil
}

// getClient возвращает HTTP-клиент с токеном авторизации
func getClient(config *oauth2.Config) *http.Client {
	// Путь к токену
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// getTokenFromWeb получает токен через веб-браузер
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// tokenFromFile загружает токен из файла
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// saveToken сохраняет токен в файл
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// LogFileUpload записывает информацию о загрузке файла в Google Sheets
func (s *SheetsService) LogFileUpload(file *models.File, username string) error {
	// Получаем текущие данные из таблицы
	resp, err := s.service.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	if err != nil {
		return fmt.Errorf("unable to retrieve data from sheet: %v", err)
	}

	// Находим первую пустую строку для записи
	nextRow := 1 // По умолчанию начинаем со строки 1 (заголовок)

	if len(resp.Values) > 0 {
		// Ищем первую пустую строку
		for i, row := range resp.Values {
			// Проверяем, является ли строка пустой или содержит только пустые ячейки
			isEmpty := true
			if len(row) > 0 {
				for _, cell := range row {
					if cell != nil && cell != "" {
						isEmpty = false
						break
					}
				}
			}

			if isEmpty {
				// Нашли пустую строку
				nextRow = i + 1 // +1 потому что индексы начинаются с 0
				break
			}

			// Если дошли до конца и не нашли пустую строку
			if i == len(resp.Values)-1 {
				nextRow = len(resp.Values) + 1
			}
		}
	}

	// Если таблица пуста, начинаем с 2 (после заголовка)
	if nextRow == 1 && len(resp.Values) > 0 {
		nextRow = 2
	}

	// Форматируем время
	timeStr := file.CreatedAt.Format("2006-01-02 15:04:05")

	// Создаем новую запись
	// Столбцы: ID файла, ID пользователя, Имя пользователя, Имя файла, Тип файла, Размер файла, Дата и время, Статус
	values := []interface{}{
		file.ID,            // ID файла
		file.UserID,        // ID пользователя
		username,           // Имя пользователя
		file.FileName,      // Имя файла
		file.FileType,      // Тип файла
		len(file.FileData), // Размер файла в байтах
		timeStr,            // Дата и время загрузки
		FileStatusActive,   // Статус файла (по умолчанию активен)
	}

	// Подготавливаем запрос на обновление
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	// Определяем диапазон для записи (строка целиком)
	writeRange := fmt.Sprintf("Лист1!A%d:H%d", nextRow, nextRow)

	// Записываем данные в таблицу
	// Используем метод Update для обновления существующих данных
	// Параметр ValueInputOption указывает, как интерпретировать входные данные
	// USER_ENTERED - как если бы пользователь вводил их в интерфейсе
	_, err = s.service.Spreadsheets.Values.Update(
		spreadsheetID,
		writeRange,
		valueRange).
		ValueInputOption("USER_ENTERED").
		Do()

	if err != nil {
		return fmt.Errorf("unable to write data to sheet: %v", err)
	}

	return nil
}

// UpdateFileStatus обновляет статус файла в Google Sheets
func (s *SheetsService) UpdateFileStatus(fileID int64, status string) error {
	// Получаем текущие данные из таблицы
	resp, err := s.service.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	if err != nil {
		return fmt.Errorf("unable to retrieve data from sheet: %v", err)
	}

	// Ищем строку с нужным ID файла
	rowIndex := -1
	for i, row := range resp.Values {
		if len(row) > 0 {
			// Проверяем, что первый столбец содержит ID файла
			if id, ok := row[0].(float64); ok && int64(id) == fileID {
				rowIndex = i + 1 // +1 потому что строки в Sheets начинаются с 1
				break
			} else if id, ok := row[0].(string); ok {
				// Пытаемся преобразовать строку в число
				var numID int64
				_, err := fmt.Sscanf(id, "%d", &numID)
				if err == nil && numID == fileID {
					rowIndex = i + 1
					break
				}
			}
		}
	}

	if rowIndex == -1 {
		return fmt.Errorf("file with ID %d not found in sheet", fileID)
	}

	// Обновляем статус файла (столбец H)
	writeRange := fmt.Sprintf("Лист1!H%d", rowIndex)
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{{status}},
	}

	_, err = s.service.Spreadsheets.Values.Update(
		spreadsheetID,
		writeRange,
		valueRange).
		ValueInputOption("USER_ENTERED").
		Do()

	if err != nil {
		return fmt.Errorf("unable to update file status: %v", err)
	}

	return nil
}

// MarkAllFilesAsDeleted отмечает все файлы пользователя как удаленные
func (s *SheetsService) MarkAllFilesAsDeleted(userID int64) error {
	// Получаем текущие данные из таблицы
	resp, err := s.service.Spreadsheets.Values.Get(spreadsheetID, readRange).Do()
	if err != nil {
		return fmt.Errorf("unable to retrieve data from sheet: %v", err)
	}

	// Собираем все строки, которые нужно обновить
	var updates []*sheets.ValueRange

	for i, row := range resp.Values {
		if len(row) > 1 { // Проверяем, что есть хотя бы два столбца (ID файла и ID пользователя)
			// Проверяем, что второй столбец содержит ID пользователя
			if id, ok := row[1].(float64); ok && int64(id) == userID {
				// Обновляем статус файла (столбец H)
				writeRange := fmt.Sprintf("Лист1!H%d", i+1) // +1 потому что строки в Sheets начинаются с 1
				valueRange := &sheets.ValueRange{
					Range:  writeRange,
					Values: [][]interface{}{{FileStatusDeleted}},
				}
				updates = append(updates, valueRange)
			} else if id, ok := row[1].(string); ok {
				// Пытаемся преобразовать строку в число
				var numID int64
				_, err := fmt.Sscanf(id, "%d", &numID)
				if err == nil && numID == userID {
					writeRange := fmt.Sprintf("Лист1!H%d", i+1)
					valueRange := &sheets.ValueRange{
						Range:  writeRange,
						Values: [][]interface{}{{FileStatusDeleted}},
					}
					updates = append(updates, valueRange)
				}
			}
		}
	}

	// Если нет файлов для обновления, выходим
	if len(updates) == 0 {
		return nil
	}

	// Выполняем пакетное обновление
	batchUpdateRequest := &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "USER_ENTERED",
		Data:             updates,
	}

	_, err = s.service.Spreadsheets.Values.BatchUpdate(
		spreadsheetID,
		batchUpdateRequest).Do()

	if err != nil {
		return fmt.Errorf("unable to batch update file statuses: %v", err)
	}

	return nil
}
