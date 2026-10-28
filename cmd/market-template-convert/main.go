package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"project/internal/model"
	"project/internal/service"
)

func convertDraft(reader io.Reader, writer io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 {
		return fmt.Errorf("legacy package exceeds 4 MiB")
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if len(envelope.Data) > 0 {
		if envelope.Code != 0 {
			return fmt.Errorf("market response is not successful")
		}
		raw = envelope.Data
	}
	var full model.MarketTemplateFullData
	if err := json.Unmarshal(raw, &full); err != nil {
		return err
	}
	converted, err := service.PreviewLegacyMarketTemplate(&full)
	if err != nil {
		return fmt.Errorf("cannot convert without data loss: %w", err)
	}
	result := struct {
		Status       string                                `json:"status"`
		Draft        *service.ConvertedLegacyNativePackage `json:"draft"`
		AssetSources map[string]string                     `json:"asset_sources,omitempty"`
		NextSteps    []string                              `json:"next_steps"`
	}{Status: "DRAFT_REQUIRES_REVIEW", Draft: converted, NextSteps: []string{
		"Review model types, access, parameters and chart resources.",
		"Copy cover assets to the target platform and replace asset:// references with target URLs.",
		"Create and publish through the native model API, then bind a device config and publish a NEW market version.",
	}}
	if len(converted.Assets) > 0 {
		result.AssetSources = converted.AssetSources
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func main() {
	input := flag.String("input", "", "downloaded legacy template JSON")
	output := flag.String("output", "", "new UTF-8 draft report file; refuses overwrite")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: market-template-convert -input legacy.json -output draft-report.json")
		os.Exit(2)
	}
	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	var report bytes.Buffer
	if err := convertDraft(f, &report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, writeErr := out.Write(report.Bytes())
	closeErr := out.Close()
	if writeErr != nil || closeErr != nil {
		fmt.Fprintln(os.Stderr, "failed to write complete report")
		os.Exit(1)
	}
	fmt.Println("Draft report created. No models, market versions or bindings were changed.")
}
