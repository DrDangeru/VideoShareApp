package main

import (
	"log"

	"VideoShareApp/internal/app"
)

func main() {
	server, err := app.New()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("server started on http://localhost:8080")
	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}
