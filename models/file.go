package models

import (
	"time"
)

// File represents a file stored in the database
type File struct {
	ID        int64     `bson:"_id"`
	UserID    int64     `bson:"user_id"`
	FileName  string    `bson:"file_name"`
	FileType  string    `bson:"file_type"`
	FileData  []byte    `bson:"file_data"`
	CreatedAt time.Time `bson:"created_at"`
}
