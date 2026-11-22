// routing-eval replays a fixed paired measurement dataset without network access.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/soulteary/yunqi/internal/routingeval"
	"io"
	"os"
)

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("routing-eval", flag.ContinueOnError)
	flags.SetOutput(out)
	input := flags.String("input", "", "fixed paired JSON dataset")
	check := flags.Bool("check", false, "fail unless evidence passes manual-review gates")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *input == "" || flags.NArg() != 0 {
		return errors.New("--input FILE is required")
	}
	f, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 {
		return errors.New("dataset too large")
	}
	d, err := routingeval.Decode(raw)
	if err != nil {
		return err
	}
	r, err := routingeval.Evaluate(d)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err = enc.Encode(r); err != nil {
		return err
	}
	if *check && r.Decision != "eligible_for_manual_review" {
		return fmt.Errorf("evaluation %s", r.Decision)
	}
	return nil
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
