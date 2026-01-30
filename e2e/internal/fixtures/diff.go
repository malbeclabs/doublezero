package fixtures

import (
	"bufio"
	"bytes"
	"slices"
	"sort"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var ignoreKeys = []string{"Last Session Update", "account"}

func DiffCLITable(want []byte, got []byte) string {
	gotMap := mapFromTable(got, ignoreKeys)
	wantMap := mapFromTable(want, ignoreKeys)

	return cmp.Diff(gotMap, wantMap, cmpopts.IgnoreMapEntries(func(key string, _ string) bool {
		return slices.Contains(ignoreKeys, key)
	}))
}

// example table
// pubkey                                       | user_type           | device   | cyoa_type  | client_ip    | tunnel_id | tunnel_net      | dz_ip        | status    | owner
// NR8fpCK7mqeFVJ3mUmhndX2JtRCymZzgQgGj5JNbGp8  | IBRL                | la2-dz01 | GREOverDIA | 1.2.3.4      | 500       | 169.254.0.2/31  | 1.2.3.4      | activated | Dc3LFdWwKGJvJcVkXhAr14kh1HS6pN7oCWrvHfQtsHGe
// 5Rm8dp4dDzR5SE3HtrqGVpqHLaPvvxDEV3EotqPBBUgS | IBRL                | la2-dz01 | GREOverDIA | 5.6.7.8      | 504       | 169.254.0.10/31 | 5.6.7.8      | activated | Dc3LFdWwKGJvJcVkXhAr14kh1HS6pN7oCWrvHfQtsHGe
func mapFromTable(output []byte, ignoreKeys []string) []map[string]string {
	var sliceOfMaps []map[string]string

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Scan()
	header := scanner.Text()
	split := strings.Split(header, "|")
	trimmed_header := make([]string, len(split))
	for i, key := range split {
		trimmed_header[i] = strings.TrimSpace(key)
	}

	for i := 0; scanner.Scan(); i++ {
		formattedMap := make(map[string]string)
		line := scanner.Text()
		split := strings.Split(line, "|")
		for i, key := range split {
			formattedMap[trimmed_header[i]] = strings.TrimSpace(key)
		}
		sliceOfMaps = append(sliceOfMaps, formattedMap)
	}

	sortMaps(sliceOfMaps, ignoreKeys)

	return sliceOfMaps
}

// ParseCLITable parses a pipe-delimited CLI table into a slice of maps keyed by column header.
func ParseCLITable(output []byte) []map[string]string {
	return mapFromTable(output, nil)
}

// canonicalKey builds a deterministic string representation of the map.
func canonicalKey(m map[string]string, excludeKeys []string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		if slices.Contains(excludeKeys, k) {
			continue
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(m[k])
		b.WriteString("|")
	}
	return b.String()
}

// sortMaps deterministically sorts the slice of maps.
func sortMaps(maps []map[string]string, excludeKeys []string) {
	sort.Slice(maps, func(i, j int) bool {
		return canonicalKey(maps[i], excludeKeys) < canonicalKey(maps[j], excludeKeys)
	})
}
