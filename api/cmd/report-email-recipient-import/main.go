package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/reportemail"
)

func main() {
	csvPath := flag.String("csv", "", "enterprise directory CSV path")
	apply := flag.Bool("apply", false, "apply uniquely matched recipients; default is dry-run")
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	flag.Parse()
	if *csvPath == "" || *databaseURL == "" {
		log.Fatal("-csv and -database-url (or DATABASE_URL) are required")
	}
	file, err := os.Open(*csvPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	database, err := db.Connect(*databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()
	results, err := reportemail.ImportRecipients(context.Background(), database, file, *apply)
	if err != nil {
		log.Fatal(err)
	}
	output := csv.NewWriter(os.Stdout)
	if err := output.Write([]string{"display_name", "email", "user_id", "status", "applied"}); err != nil {
		log.Fatal(err)
	}
	for _, result := range results {
		if err := output.Write([]string{result.DisplayName, result.Email, result.UserID, result.Status, fmt.Sprint(result.Applied)}); err != nil {
			log.Fatal(err)
		}
	}
	output.Flush()
	if err := output.Error(); err != nil {
		log.Fatal(err)
	}
}
