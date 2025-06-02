package db

import (
	"database/sql"
	"telegram-bot/models"

	_ "github.com/mattn/go-sqlite3"
)

// DB is a wrapper around sql.DB
type DB struct {
	*sql.DB
}

// NewDB creates a new database connection
func NewDB(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &DB{db}, nil
}

// InitDB initializes the database with required tables
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

	_, err := db.Exec(query)
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

	_, err := db.Exec(query, user.ID, user.Username, user.FirstName, user.LastName, user.Phone, user.IsAdmin)
	return err
}

// GetUser retrieves a user by ID
func (db *DB) GetUser(id int64) (*models.User, error) {
	query := `SELECT id, username, first_name, last_name, phone, is_admin FROM users WHERE id = ?`

	var user models.User
	err := db.QueryRow(query, id).Scan(
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
	_, err := db.Exec(query, phone, id)
	return err
}

// SetUserAdmin sets a user's admin status
func (db *DB) SetUserAdmin(id int64, isAdmin bool) error {
	query := `UPDATE users SET is_admin = ? WHERE id = ?`
	_, err := db.Exec(query, isAdmin, id)
	return err
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
