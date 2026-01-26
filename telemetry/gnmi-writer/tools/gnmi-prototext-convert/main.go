// gnmi-prototext-convert converts raw gNMI Notification prototext files into
// SubscribeResponse format suitable for gnmi-writer testdata.
//
// Raw gNMI GET responses return Notification messages directly, but gnmi-writer
// tests expect SubscribeResponse wrappers (with the Notification in the 'update' field).
//
// This tool handles multiple input formats:
//   - Raw Notification prototext (starts with timestamp:, prefix:, etc.)
//   - Wrapped format with "notification: { ... }" (non-standard but common from some tools)
//   - Already-formatted SubscribeResponse (will just update the target)
//
// Usage:
//
//	cat raw.prototext | go run ./tools/gnmi-prototext-convert --target DEVICE_PUBKEY > formatted.prototext
//	go run ./tools/gnmi-prototext-convert --target DEVICE_PUBKEY --input raw.prototext > formatted.prototext
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/protobuf/encoding/prototext"
)

func main() {
	target := flag.String("target", "", "device pubkey to set in prefix.target (required)")
	input := flag.String("input", "", "input file (default: stdin)")
	flag.Parse()

	if *target == "" {
		fmt.Fprintln(os.Stderr, "error: --target is required")
		flag.Usage()
		os.Exit(1)
	}

	var r io.Reader = os.Stdin
	if *input != "" {
		f, err := os.Open(*input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening input file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		r = f
	}

	data, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading input: %v\n", err)
		os.Exit(1)
	}

	// Check if the file uses the non-standard "notification: { ... }" wrapper format.
	// This happens when capturing raw gNMI output with some tools.
	// We need to transform it to use "update: { ... }" instead.
	notificationWrapper := regexp.MustCompile(`(?s)^\s*notification\s*:\s*\{(.+)\}\s*$`)
	if matches := notificationWrapper.FindSubmatch(data); matches != nil {
		// Extract the inner content and wrap it as "update: { ... }"
		inner := bytes.TrimSpace(matches[1])
		data = []byte(fmt.Sprintf("update: {\n%s\n}", string(inner)))
	}

	// Try to parse as SubscribeResponse first
	var resp gpb.SubscribeResponse
	if err := prototext.Unmarshal(data, &resp); err == nil {
		// It's a SubscribeResponse, update the target
		if resp.GetUpdate() != nil {
			if resp.GetUpdate().Prefix == nil {
				resp.GetUpdate().Prefix = &gpb.Path{}
			}
			resp.GetUpdate().Prefix.Target = *target
		}
		output, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(&resp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error marshaling output: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(string(output))
		return
	}

	// Try to parse as raw Notification
	var notification gpb.Notification
	if err := prototext.Unmarshal(data, &notification); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing prototext: %v\n", err)
		os.Exit(1)
	}

	// Set the target in the prefix
	if notification.Prefix == nil {
		notification.Prefix = &gpb.Path{}
	}
	notification.Prefix.Target = *target

	// Wrap in SubscribeResponse
	resp = gpb.SubscribeResponse{
		Response: &gpb.SubscribeResponse_Update{
			Update: &notification,
		},
	}

	output, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(&resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling output: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(output))
}
