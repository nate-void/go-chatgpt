package mysql

import (
	"chatgpt-web/pkg/config"
	"database/sql"
	"log"
	"time"
)
import _ "github.com/go-sql-driver/mysql"

var db *sql.DB

func InitDB() {
	var err error
	cfg := config.GetConfig()
	if cfg.DB.DSN == "" {
		log.Fatalln("data source name empty")
	}

	if db, err = sql.Open("mysql", cfg.DB.DSN); err != nil {
		log.Fatalln(err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Minute * 30)
}

func GetDB() *sql.DB {
	return db
}
