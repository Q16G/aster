package testdata

import (
	"fmt"
	"log"
)

// ruleid: go-misc-sensitive-data-in-log
func logPassword(password string) {
	log.Printf("User password: %s", password)
}

// ruleid: go-misc-sensitive-data-in-log
func logToken(accessToken string) {
	log.Println("Token:", accessToken)
}

// ruleid: go-misc-sensitive-data-in-log
func logSecretKey(secretKey string) {
	fmt.Printf("Key: %s", secretKey)
}

// ruleid: go-misc-sensitive-data-in-log
func logApiKey(apiKey string) {
	log.Printf("API key = %s", apiKey)
}

// ok: go-misc-sensitive-data-in-log
func logNormal(username string) {
	log.Printf("User logged in: %s", username)
}

// ok: go-misc-sensitive-data-in-log
func logMessage() {
	log.Println("Please enter your password")
}

// ok: go-misc-sensitive-data-in-log
func logUserId(userId int) {
	log.Printf("Processing user: %d", userId)
}
