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
	sqlStr := "insert into chat_records(user_msg,ai_msg,create_at)"
	res, err := data.db.Exec(sqlStr, cr.UserMassage, cr.AIMessage, cr.CreateAt)
	if err != nil {
		log.Println(err)
		return
	}
	cr.ID, _ = res.LastInsertId()
	return nil
}
