package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"telegram-bot/models"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DB is a wrapper around both SQLite and MongoDB
type DB struct {
	SQLite *sql.DB
	mongo  *mongo.Client
	files  *mongo.Collection
}

// NewDB creates new database connections
func NewDB(sqlitePath, mongoURI string) (*DB, error) {
	// Connect to SQLite with configuration
	sqliteDB, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		return nil, err
	}

	// Configure SQLite for better reliability
	sqliteDB.SetMaxOpenConns(10) // Allow more connections
	sqliteDB.SetConnMaxLifetime(time.Hour)
	sqliteDB.SetMaxIdleConns(5)

	// Set busy timeout to handle locks
	_, err = sqliteDB.Exec("PRAGMA busy_timeout=10000") // 10 seconds timeout
	if err != nil {
		sqliteDB.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Connect to MongoDB
	mongoClient, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		sqliteDB.Close()
		return nil, err
	}

	// Get files collection
	database := mongoClient.Database("telegram_bot")
	files := database.Collection("files")

	return &DB{
		SQLite: sqliteDB,
		mongo:  mongoClient,
		files:  files,
	}, nil
}

// InitDB initializes the SQLite database with required tables
func (db *DB) InitDB() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		username TEXT,
		first_name TEXT,
		last_name TEXT,
		phone TEXT,
		is_admin BOOLEAN DEFAULT 0
	);
	`

	_, err := db.SQLite.Exec(query)
	return err
}

// SaveUser saves or updates a user in the database
func (db *DB) SaveUser(user *models.User) error {
	query := `
	INSERT INTO users (id, username, first_name, last_name, phone, is_admin)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		username = excluded.username,
		first_name = excluded.first_name,
		last_name = excluded.last_name,
		phone = excluded.phone,
		is_admin = excluded.is_admin
	`

	_, err := db.SQLite.Exec(query, user.ID, user.Username, user.FirstName, user.LastName, user.Phone, user.IsAdmin)
	return err
}

// GetUser retrieves a user by ID
func (db *DB) GetUser(id int64) (*models.User, error) {
	query := `SELECT id, username, first_name, last_name, phone, is_admin FROM users WHERE id = ?`

	var user models.User
	err := db.SQLite.QueryRow(query, id).Scan(
		&user.ID, &user.Username, &user.FirstName, &user.LastName, &user.Phone, &user.IsAdmin,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// UpdateUserPhone updates a user's phone number
func (db *DB) UpdateUserPhone(id int64, phone string) error {
	query := `UPDATE users SET phone = ? WHERE id = ?`
	_, err := db.SQLite.Exec(query, phone, id)
	return err
}

// SetUserAdmin sets a user's admin status
func (db *DB) SetUserAdmin(id int64, isAdmin bool) error {
	query := `UPDATE users SET is_admin = ? WHERE id = ?`
	_, err := db.SQLite.Exec(query, isAdmin, id)
	return err
}

// SaveFile saves a file to MongoDB
func (db *DB) SaveFile(file *models.File) error {
	// Check for duplicate file
	var existingFile models.File
	err := db.files.FindOne(context.Background(), bson.M{
		"user_id":   file.UserID,
		"file_name": file.FileName,
		"file_type": file.FileType,
	}).Decode(&existingFile)

	if err == nil {
		return errors.New("file already exists")
	} else if err != mongo.ErrNoDocuments {
		return err
	}

	// Get next ID
	var lastFile models.File
	opts := options.FindOne().SetSort(bson.M{"_id": -1})
	err = db.files.FindOne(context.Background(), bson.M{}, opts).Decode(&lastFile)
	if err != nil && err != mongo.ErrNoDocuments {
		return err
	}

	file.ID = lastFile.ID + 1
	_, err = db.files.InsertOne(context.Background(), file)
	return err
}

// GetFile retrieves a file by ID
func (db *DB) GetFile(id int64) (*models.File, error) {
	var file models.File
	err := db.files.FindOne(context.Background(), bson.M{"_id": id}).Decode(&file)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &file, nil
}

// GetUserFiles retrieves all files for a user
func (db *DB) GetUserFiles(userID int64) ([]*models.File, error) {
	cursor, err := db.files.Find(context.Background(), bson.M{"user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(context.Background())

	var files []*models.File
	if err = cursor.All(context.Background(), &files); err != nil {
		return nil, err
	}
	return files, nil
}

// DeleteFile deletes a file by ID
func (db *DB) DeleteFile(id int64) error {
	_, err := db.files.DeleteOne(context.Background(), bson.M{"_id": id})
	return err
}

// GetAllFiles retrieves all files from the database
func (db *DB) GetAllFiles() ([]*models.File, error) {
	cursor, err := db.files.Find(context.Background(), bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(context.Background())

	var files []*models.File
	if err = cursor.All(context.Background(), &files); err != nil {
		return nil, err
	}
	return files, nil
}

// UpdateAdminStatuses updates admin status for all users based on the provided admin map
func (db *DB) UpdateAdminStatuses(admins map[string]bool) error {
	// Start a transaction
	tx, err := db.SQLite.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// First, set all users to non-admin
	_, err = tx.Exec("UPDATE users SET is_admin = 0")
	if err != nil {
		return fmt.Errorf("failed to reset admin statuses: %w", err)
	}

	// Then update only those who are admins
	for username, isAdmin := range admins {
		if isAdmin {
			_, err = tx.Exec("UPDATE users SET is_admin = 1 WHERE username = ?", username)
			if err != nil {
				return fmt.Errorf("failed to update admin status for %s: %w", username, err)
			}
		}
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Close closes all database connections
func (db *DB) Close() error {
	if err := db.SQLite.Close(); err != nil {
		return err
	}
	return db.mongo.Disconnect(context.Background())
}
