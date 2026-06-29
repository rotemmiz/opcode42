// Command diff compares two conformance result files (the "expected" target,
// normally opencode, and the "actual" target, normally opcode42) and reports
// structural differences in the plan 12 §d format. It exits non-zero on any
// difference not covered by the known-divergence registry.
//
//	go run ./conformance/cmd/diff [-divergences FILE] expected.json actual.json
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rotemmiz/opcode42/conformance/result"
)

func main() {
	divPath := flag.String("divergences", "conformance/known-divergences.json", "path to the known-divergence registry")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: diff [-divergences FILE] expected.json actual.json")
		os.Exit(2)
	}

	expected, err := result.Load(flag.Arg(0))
	if err != nil {
		fatal(err)
	}
	actual, err := result.Load(flag.Arg(1))
	if err != nil {
		fatal(err)
	}
	divs, err := loadDivergences(*divPath)
	if err != nil {
		fatal(err)
	}

	report := Compare(expected, actual)
	os.Exit(report.Print(os.Stdout, divs))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "diff:", err)
	os.Exit(2)
}
