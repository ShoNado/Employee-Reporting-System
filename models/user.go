package models

// User represents a Telegram bot user
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Phone     string `json:"phone"`
	IsAdmin   bool   `json:"is_admin"`
}
