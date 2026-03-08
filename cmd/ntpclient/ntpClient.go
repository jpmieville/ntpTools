package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"ntptools/pkg/ntp"
)

func main() {
	server := flag.String("server", ntp.DefaultServers[0], "NTP server hostname or IP")
	version := flag.Uint("version", 4, "NTP version (3 or 4)")
	timeout := flag.Duration("timeout", ntp.DefaultTimeout, "Socket timeout")
	listServers := flag.Bool("list-servers", false, "List available default NTP servers")
	jsonOutput := flag.Bool("json", false, "Output result as JSON")
	compactOutput := flag.Bool("compact", false, "Output compact single-line JSON (implies -json)")
	flag.BoolVar(&ntp.Verbose, "verbose", false, "Enable verbose debug output")
	flag.Parse()

	if *listServers {
		fmt.Println("Available default NTP servers:")
		for _, s := range ntp.DefaultServers {
			fmt.Printf("  - %s\n", s)
		}
		os.Exit(0)
	}

	client := ntp.NewClient(*server, uint8(*version), *timeout)

	result, err := client.FetchTime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput || *compactOutput {
		enc := json.NewEncoder(os.Stdout)
		if !*compactOutput {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		ntp.PrintResult(result)
	}
}
