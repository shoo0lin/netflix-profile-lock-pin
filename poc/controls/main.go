// Command controls runs the negative controls that prove the lock is real and
// the PIN is the reason it opened:
//
//	--mode no-pin   select the locked tile, submit nothing   -> expect BLOCKED
//	--mode wrong    select the locked tile, submit 0000      -> expect BLOCKED
//	--mode right    select the locked tile, submit the PIN   -> expect UNLOCKED
//
// Run all three with the same cookie file and the same target GUID. `right` is
// the one that must differ; if `wrong` also switches profile, the lock is
// decorative and the finding is bigger -- or the harness is wrong. Check in
// that order.
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

const usage = `Negative controls: prove the lock is real and the PIN is the reason it opened.

  --mode no-pin   select the locked tile, submit nothing   -> expect BLOCKED
  --mode wrong    select the locked tile, submit 0000      -> expect BLOCKED
  --mode right    select the locked tile, submit the PIN   -> expect UNLOCKED

  go run ./poc/controls --cookies cookies-kids.txt --target <protected-guid> --mode wrong --confirm-own-account
`

var modes = []string{"no-pin", "wrong", "right"}

func main() { os.Exit(run()) }

func run() int {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	cookiesPath := flag.String("cookies", "", "one-line cookie string exported from your browser")
	target := flag.String("target", "", "GUID of the PIN-locked profile")
	mode := flag.String("mode", "", "one of: no-pin, wrong, right")
	pin := flag.String("pin", "0000", "PIN to submit (mode wrong/right)")
	profileDir := flag.String("profile-dir", "/tmp/netflix-lockpin-controls", "persistent Chromium profile directory")
	headed := flag.Bool("headed", false, "run Chromium headed instead of headless")
	confirm := flag.Bool("confirm-own-account", false, "confirm this is your own account")
	flag.Parse()

	if missing := required(*cookiesPath, *target, *mode); missing != "" {
		fmt.Fprintf(os.Stderr, "the following arguments are required: %s\n", missing)
		flag.Usage()
		return 2
	}
	if !validMode(*mode) {
		fmt.Fprintf(os.Stderr, "argument --mode: invalid choice: %q (choose from %q, %q, %q)\n",
			*mode, modes[0], modes[1], modes[2])
		flag.Usage()
		return 2
	}
	profiles.RequireOwnAccount(*confirm)

	cookieString, err := profiles.ReadCookieFile(*cookiesPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	s, err := browser.Open(*profileDir+"-"+*mode, *headed, cookieString)
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
	fmt.Printf("[%s] start pg=%s\n", *mode, pyOptional(start))

	if err := s.GotoGate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	browser.Wait(3000)

	tile := fmt.Sprintf(`[data-uia="profile-selector+tile-%s"]`, *target)
	if err := s.ClickFirst(tile, 8*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	browser.Wait(4000)

	prompt, err := s.AnyPrompt()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("[%s] PIN prompt visible: %s\n", *mode, profiles.PyBool(prompt))

	if *mode == "wrong" || *mode == "right" {
		field, err := s.FindPrompt()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if field != "" {
			if err := s.Fill(field, *pin); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return 1
			}
			_ = s.Submit()
		}
		browser.Wait(5000)
	}

	final, err := s.PG()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	url, _, err := s.Location()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("[%s] final pg=%s url=%s\n", *mode, pyOptional(final), url)
	switched := final == *target
	result := "BLOCKED"
	if switched {
		result = "SWITCHED(!!)"
	}
	fmt.Printf("[%s] RESULT: %s\n", *mode, result)
	expected := *mode != "right"
	if switched == expected {
		return 0
	}
	return 1
}

func required(values ...string) string {
	names := []string{"--cookies", "--target", "--mode"}
	var missing []string
	for i, v := range values {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, names[i])
		}
	}
	return strings.Join(missing, ", ")
}

func validMode(mode string) bool {
	for _, m := range modes {
		if mode == m {
			return true
		}
	}
	return false
}

func pyOptional(value string) string {
	if value == "" {
		return "None"
	}
	return value
}
