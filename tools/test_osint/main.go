package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"info-bot-go/internal/osint"
)

func main() {
	rawKeys := os.Getenv("GEMINI_API_KEY")
	if rawKeys == "" {
		rawKeys = os.Getenv("GOOGLE_API_KEY")
	}
	if rawKeys == "" {
		log.Fatal("GEMINI_API_KEY or GOOGLE_API_KEY is required")
	}

	keys := strings.Split(rawKeys, ",")
	for i, k := range keys {
		keys[i] = strings.TrimSpace(k)
	}

	agency := strings.Join(os.Args[1:], " ")
	if agency == "" {
		agency = "Запорізький апеляційний суд"
	}

	fmt.Println("=== OSINT Email Finder Self-Test ===")
	fmt.Printf("Keys:    %d configured\n", len(keys))
	fmt.Printf("Agency:  %s\n", agency)
	fmt.Println()

	finder := osint.NewFinder(keys)
	result, err := finder.FindEmail(agency)
	if err != nil {
		log.Fatalf("OSINT search failed: %v", err)
	}

	fmt.Println("=== Result ===")
	fmt.Printf("Email:      %s\n", result.Email)
	fmt.Printf("Agency:     %s\n", result.AgencyName)
	fmt.Printf("Confidence: %s\n", result.Confidence)

	if len(result.SourceURLs) > 0 {
		fmt.Println("\nSources:")
		for _, u := range result.SourceURLs {
			fmt.Printf("  - %s\n", u)
		}
	}

	fmt.Println("\nRaw AI response:")
	fmt.Println(result.RawResponse)

	if result.Email == "" {
		fmt.Println("\n❌ Email not found")
		os.Exit(1)
	}

	fmt.Println("\n✅ Email found!")
}
