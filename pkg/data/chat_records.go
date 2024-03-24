package data

import (
	"database/sql"
	"log"
)

type ChatRecord struct {
	ID          int64
	UserMassage string
	AIMessage   string
	CreateAt    int64
}

type IChatRecordsData interface {
	Add(*ChatRecord) error
	AddSensitive(cr *ChatRecord) error
}
type ChatRecordsData struct {
	db *sql.DB
}

func NewChatRecordsData(db *sql.DB) IChatRecordsData {
	return &ChatRecordsData{
		db: db,
	}
}

func (data *ChatRecordsData) Add(cr *ChatRecord) (err error) {
	sqlStr := "INSERT INTO chat_records(user_msg, ai_msg, create_at) VALUES(?, ?, ?)"
	res, err := data.db.Exec(sqlStr, cr.UserMassage, cr.AIMessage, cr.CreateAt)
	if err != nil {
		log.Println(err)
		return err
	}
	cr.ID, _ = res.LastInsertId()
	return nil
}

func (data *ChatRecordsData) AddSensitive(cr *ChatRecord) (err error) {
	sqlStr := "INSERT INTO chat_sensitive _records(user_msg, ai_msg, create_at) VALUES(?, ?, ?)"
	res, err := data.db.Exec(sqlStr, cr.UserMassage, cr.AIMessage, cr.CreateAt)
	if err != nil {
		log.Println(err)
		return err
	}
	cr.ID, _ = res.LastInsertId()
	return nil
}
