package main

import (
	"flag"
	"fmt"
	"net"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"info-bot-go/internal/directory"
)

type checkResult struct {
	r     directory.Recipient
	valid bool
	hasMX bool
	smtp  bool
	err   string
}

func main() {
	smtp := flag.Bool("smtp", false, "perform SMTP ping (may be unreliable from cloud IPs)")
	delay := flag.Duration("delay", 1500*time.Millisecond, "delay between checks")
	timeout := flag.Duration("timeout", 5*time.Second, "timeout for each SMTP check")
	flag.Parse()

	fmt.Println("=== Email Directory Verification ===")
	fmt.Printf("SMTP ping: %v\n", *smtp)
	if *smtp {
		fmt.Printf("Delay:     %s\n", *delay)
		fmt.Printf("Timeout:   %s\n", *timeout)
	}
	fmt.Println()

	recipients := directory.All().AllRecipients()
	fmt.Printf("Total addresses to check: %d\n\n", len(recipients))

	results := make([]checkResult, 0, len(recipients))
	var validCount, invalidCount, unknownCount int

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var interrupted bool
	go func() {
		<-sigCh
		interrupted = true
	}()

	for i, r := range recipients {
		if interrupted {
			fmt.Println("\n\n⚠️ Interrupted, printing partial results...")
			break
		}

		fmt.Printf("[%d/%d] %-40s ", i+1, len(recipients), r.Email)

		_, err := mail.ParseAddress(r.Email)
		if err != nil {
			fmt.Println("❌")
			fmt.Printf("       Syntax error: %v\n", err)
			fmt.Printf("       %s (%s)\n", r.Name, r.Category)
			invalidCount++
			results = append(results, checkResult{r: r, err: "invalid syntax"})
			time.Sleep(*delay)
			continue
		}

		domain := r.Email[strings.LastIndex(r.Email, "@")+1:]

		mxRecords, err := net.LookupMX(domain)
		if err != nil || len(mxRecords) == 0 {
			fmt.Println("❌ No MX")
			fmt.Printf("       Domain: %s\n", domain)
			fmt.Printf("       %s (%s)\n", r.Name, r.Category)
			invalidCount++
			results = append(results, checkResult{r: r, err: fmt.Sprintf("no MX records: %v", err)})
			time.Sleep(*delay)
			continue
		}

		if !*smtp {
			fmt.Println("✅")
			validCount++
			results = append(results, checkResult{r: r, valid: true, hasMX: true, smtp: false})
			time.Sleep(*delay)
			continue
		}

		mxHost := mxRecords[0].Host
		mxHost = strings.TrimSuffix(mxHost, ".")

		ok, desc := smtpPing(mxHost, r.Email, *timeout)
		if ok {
			fmt.Println("✅")
			validCount++
			results = append(results, checkResult{r: r, valid: true, hasMX: true, smtp: true})
		} else {
			if desc == "refused" {
				fmt.Println("❌ Refused")
				fmt.Printf("       MX: %s\n", mxHost)
				fmt.Printf("       %s (%s)\n", r.Name, r.Category)
				invalidCount++
				results = append(results, checkResult{r: r, hasMX: true, err: "SMTP refused"})
			} else {
				fmt.Println("⚠️")
				fmt.Printf("       %s\n", desc)
				fmt.Printf("       MX: %s\n", mxHost)
				fmt.Printf("       %s (%s)\n", r.Name, r.Category)
				unknownCount++
				results = append(results, checkResult{r: r, hasMX: true, err: desc})
			}
		}

		time.Sleep(*delay)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("  RESULTS SUMMARY")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  ✅ Valid:      %d\n", validCount)
	fmt.Printf("  ❌ Invalid:    %d\n", invalidCount)
	fmt.Printf("  ⚠️  Unknown:    %d\n", unknownCount)
	fmt.Printf("  Total:         %d\n", len(recipients))

	if invalidCount > 0 {
		fmt.Println()
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println("  DEAD ADDRESSES (NEED OSINT)")
		fmt.Println(strings.Repeat("─", 60))
		for _, res := range results {
			if res.valid {
				continue
			}
			fmt.Printf("  ❌ %-40s\n", res.r.Email)
			fmt.Printf("    Name: %s\n", res.r.Name)
			fmt.Printf("    Issue: %s\n", res.err)
		}
	}

	fmt.Println()
	fmt.Println("Done.")
}

func smtpPing(host, email string, timeout time.Duration) (bool, string) {
	addr := net.JoinHostPort(host, "25")
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false, fmt.Sprintf("connect failed: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	conn.SetDeadline(deadline)

	readResp := func() (int, string, error) {
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		if err != nil {
			return 0, "", err
		}
		line := strings.TrimSpace(string(buf[:n]))
		code := 0
		fmt.Sscanf(line, "%d", &code)
		return code, line, nil
	}

	send := func(cmd string) error {
		_, err := conn.Write([]byte(cmd + "\r\n"))
		return err
	}

	code, _, err := readResp()
	if err != nil {
		return false, fmt.Sprintf("banner: %v", err)
	}

	if err := send("EHLO verify.local"); err != nil {
		return false, fmt.Sprintf("EHLO send: %v", err)
	}
	code, _, err = readResp()
	if err != nil {
		return false, fmt.Sprintf("EHLO resp: %v", err)
	}
	if code != 250 {
		conn.Close()
		return false, "EHLO rejected"
	}

	if err := send("MAIL FROM:<check@verify.test>"); err != nil {
		return false, fmt.Sprintf("MAIL FROM: %v", err)
	}
	code, _, err = readResp()
	if err != nil {
		return false, fmt.Sprintf("MAIL FROM resp: %v", err)
	}
	if code != 250 {
		return false, fmt.Sprintf("MAIL FROM rejected: %d", code)
	}

	if err := send(fmt.Sprintf("RCPT TO:<%s>", email)); err != nil {
		return false, fmt.Sprintf("RCPT TO send: %v", err)
	}
	code, _, err = readResp()
	if err != nil {
		return false, fmt.Sprintf("RCPT TO resp: %v", err)
	}

	send("QUIT")

	switch {
	case code == 250 || code == 251 || code == 252:
		return true, ""
	case code == 550 || code == 551 || code == 553 || code == 554:
		return false, "refused"
	case code >= 450 && code <= 452:
		return false, fmt.Sprintf("temp fail: %d", code)
	default:
		return false, fmt.Sprintf("SMTP code: %d", code)
	}
}
