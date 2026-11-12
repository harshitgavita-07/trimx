package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/harshitgavita-07/trimx"
)

func main() {
	var query, point, mode string
	var file string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--query":
			i++
			if i < len(args) {
				query = args[i]
			}
		case "--point":
			i++
			if i < len(args) {
				point = args[i]
			}
		case "--mode":
			i++
			if i < len(args) {
				mode = args[i]
			}
		case "--help", "-h":
			fmt.Fprintln(os.Stderr, "usage: trimx --query QUERY [--point POINT] [--mode auto|text|json] [FILE]   (reads stdin without FILE)")
			os.Exit(0)
		default:
			if file == "" && !strings.HasPrefix(args[i], "--") {
				file = args[i]
			}
		}
	}
	if strings.TrimSpace(query) == "" {
		fmt.Fprintln(os.Stderr, "trimx: --query is required")
		os.Exit(2)
	}
	var data []byte
	var err error
	if file != "" {
		data, err = os.ReadFile(file)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimx:", err)
		os.Exit(1)
	}
	m := trimx.ModeAuto
	switch mode {
	case "", "auto":
	case "text":
		m = trimx.ModeText
	case "json":
		m = trimx.ModeJSON
	default:
		fmt.Fprintln(os.Stderr, "trimx: unknown --mode", mode)
		os.Exit(2)
	}
	res := trimx.Compress(string(data), query, trimx.Options{Point: point, Mode: m})
	fmt.Print(res.Output)
	switch {
	case res.Unchanged:
		fmt.Fprintf(os.Stderr, "trimx: unchanged (%s)\n", res.Reason)
	default:
		unit := "lines"
		if res.Mode == trimx.ModeJSON {
			unit = "records"
		}
		note := ""
		if res.ExpandedQuery != "" {
			note = ", query expanded"
		}
		fmt.Fprintf(os.Stderr, "trimx: kept %d %s, removed %d, %d -> %d bytes%s\n",
			res.KeptLines, unit, res.Removed, len(data), len(res.Output), note)
	}
}
