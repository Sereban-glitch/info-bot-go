package main

import (
	"fmt"
	"os"

	"info-bot-go/internal/directory"
)

func main() {
	sessionDir := os.Getenv("SESSION_DIR")
	if sessionDir == "" {
		sessionDir = "/tmp/test_osint_cache"
	}
	_ = os.MkdirAll(sessionDir, 0755)

	fmt.Println("=== Persistent Learning Cache Self-Test ===")
	fmt.Printf("Session dir: %s\n", sessionDir)
	fmt.Println()

	// Step 1: Create directory and load (should be empty first time)
	dir := directory.All()
	dir.LoadLearned(sessionDir)

	fmt.Printf("Before: %d built-in + %d learned\n", len(dir.AllRecipients())-dir.LearnedCount(), dir.LearnedCount())

	// Step 2: Add a learned agency
	id := dir.AddLearned(sessionDir, "Тестовий Орган Для Кешу", "test@cache.gov.ua")
	fmt.Printf("Added learned: %s -> test@cache.gov.ua\n", id)
	fmt.Printf("After add: %d learned\n", dir.LearnedCount())

	// Step 3: Search should now find it
	results := dir.Search("тестовий орган")
	fmt.Printf("Search \"тестовий орган\": %d results\n", len(results))
	if len(results) > 0 {
		fmt.Printf("  Found: %s <%s>\n", results[0].Name, results[0].Email)
	}

	// Step 4: Create a NEW directory instance (simulating restart) and load
	dir2 := directory.All()
	dir2.LoadLearned(sessionDir)
	fmt.Printf("\nAfter simulated restart: %d learned\n", dir2.LearnedCount())

	results2 := dir2.Search("тестовий орган")
	fmt.Printf("Search after restart: %d results\n", len(results2))
	if len(results2) > 0 {
		fmt.Printf("  Found: %s <%s>\n", results2[0].Name, results2[0].Email)
		fmt.Println("\n✅ Cache survives restart!")
	} else {
		fmt.Println("\n❌ Cache lost after restart")
		os.Exit(1)
	}

	// Cleanup test file
	os.Remove(sessionDir + "/learned_agencies.json")
	fmt.Println("(Test file cleaned up)")
}
