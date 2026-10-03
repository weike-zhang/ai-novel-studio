package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

func main() {
	if len(os.Args) != 2 {
		panic("usage: migrate <up|down|status>")
	}
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		panic("MYSQL_DSN is required")
	}
	directory := os.Getenv("MIGRATIONS_DIR")
	if directory == "" {
		directory = "db/migrations"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if err := goose.SetDialect("mysql"); err != nil {
		panic(err)
	}
	switch os.Args[1] {
	case "up":
		err = goose.Up(db, directory)
	case "down":
		err = goose.Down(db, directory)
	case "status":
		err = goose.Status(db, directory)
	default:
		err = fmt.Errorf("unsupported migration command %q", os.Args[1])
	}
	if err != nil {
		panic(err)
	}
}
