package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"github.com/Gu1llaum-3/vigil"
	"github.com/Gu1llaum-3/vigil/agent"
	"github.com/Gu1llaum-3/vigil/agent/health"
	"github.com/Gu1llaum-3/vigil/agent/utils"
	gossh "golang.org/x/crypto/ssh"
)

// cli options
type cmdOptions struct {
	key    string // key is the hub's public key(s) for WebSocket authentication.
	hubURL string // hubURL is the URL of the hub.
	token  string // token is the token to use for authentication.
}

// parse parses the command line flags and populates the config struct.
// It returns true if a subcommand was handled and the program should exit.
func (opts *cmdOptions) parse() bool {
	subcommand := ""
	if len(os.Args) > 1 {
		subcommand = os.Args[1]
	}

	// Subcommands that don't require any pflag parsing
	switch subcommand {
	case "health":
		err := health.Check()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print("ok")
		return true
	case "fingerprint":
		handleFingerprint()
		return true
	}

	pflag.StringVarP(&opts.key, "key", "k", "", "Hub's public key(s) for WebSocket authentication")
	pflag.StringVarP(&opts.hubURL, "url", "u", "", "URL of the hub")
	pflag.StringVarP(&opts.token, "token", "t", "", "Token to use for authentication")
	version := pflag.BoolP("version", "v", false, "Show version information")
	help := pflag.BoolP("help", "h", false, "Show this help message")

	// Convert old single-dash long flags to double-dash for backward compatibility
	flagsToConvert := []string{"key", "url", "token"}
	for i, arg := range os.Args {
		for _, flag := range flagsToConvert {
			singleDash := "-" + flag
			doubleDash := "--" + flag
			if arg == singleDash {
				os.Args[i] = doubleDash
				break
			} else if strings.HasPrefix(arg, singleDash+"=") {
				os.Args[i] = doubleDash + arg[len(singleDash):]
				break
			}
		}
	}

	pflag.Usage = func() {
		builder := strings.Builder{}
		builder.WriteString("Usage: ")
		builder.WriteString(os.Args[0])
		builder.WriteString(" [command] [flags]\n")
		builder.WriteString("\nCommands:\n")
		builder.WriteString("  fingerprint  View or reset the agent fingerprint\n")
		builder.WriteString("  health       Check if the agent is running\n")
		builder.WriteString("\nFlags:\n")
		fmt.Print(builder.String())
		pflag.PrintDefaults()
	}

	// Parse all arguments with pflag
	pflag.Parse()

	// Must run after pflag.Parse()
	switch {
	case *version:
		fmt.Println(app.AgentBinary, app.Version)
		return true
	case *help || subcommand == "help":
		pflag.Usage()
		return true
	}

	// Set environment variables from CLI flags (if provided)
	if opts.hubURL != "" {
		os.Setenv("HUB_URL", opts.hubURL)
	}
	if opts.token != "" {
		os.Setenv("TOKEN", opts.token)
	}
	return false
}

// loadPublicKeys loads the hub's public keys from the command line flag, environment variable, or key file.
func (opts *cmdOptions) loadPublicKeys() ([]gossh.PublicKey, error) {
	// Try command line flag first
	if opts.key != "" {
		return agent.ParseKeys(opts.key)
	}

	// Try environment variable
	if key, ok := utils.GetEnv("KEY"); ok && key != "" {
		return agent.ParseKeys(key)
	}

	// Try key file
	keyFile, ok := utils.GetEnv("KEY_FILE")
	if !ok {
		return nil, nil
	}

	pubKey, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	return agent.ParseKeys(string(pubKey))
}

// handleFingerprint handles the "fingerprint" command with subcommands "view" and "reset".
func handleFingerprint() {
	subCmd := ""
	if len(os.Args) > 2 {
		subCmd = os.Args[2]
	}

	switch subCmd {
	case "", "view":
		dataDir, _ := agent.GetDataDir()
		fp := agent.GetFingerprint(dataDir)
		fmt.Println(fp)
	case "help", "-h", "--help":
		fmt.Print(fingerprintUsage())
	case "reset":
		dataDir, err := agent.GetDataDir()
		if err != nil {
			log.Fatal(err)
		}
		if err := agent.DeleteFingerprint(dataDir); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Fingerprint reset. A new one will be generated on next start.")
	default:
		log.Fatalf("Unknown command: %q\n\n%s", subCmd, fingerprintUsage())
	}
}

func fingerprintUsage() string {
	return fmt.Sprintf("Usage: %s fingerprint [view|reset]\n\nCommands:\n  view   Print fingerprint (default)\n  reset  Reset saved fingerprint\n", os.Args[0])
}

func main() {
	var opts cmdOptions
	if opts.parse() {
		return
	}

	keys, err := opts.loadPublicKeys()
	if err != nil {
		log.Fatal("Failed to load public keys:", err)
	}

	a, err := agent.NewAgent()
	if err != nil {
		log.Fatal("Failed to create agent: ", err)
	}

	if err := a.Start(keys); err != nil {
		log.Fatal("Failed to start agent: ", err)
	}
}
