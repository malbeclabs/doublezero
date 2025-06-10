package fixtures

import (
	"bufio"
	"bytes"
	"slices"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func DiffCLITable(want []byte, got []byte) string {
	gotMap := mapFromTable(got)
	wantMap := mapFromTable(want)

	ignoreKeys := []string{"Last Session Update"}

	return cmp.Diff(gotMap, wantMap, cmpopts.IgnoreMapEntries(func(key string, _ string) bool {
		return slices.Contains(ignoreKeys, key)
	}))
}

// example table
// pubkey                                       | user_type           | device   | cyoa_type  | client_ip    | tunnel_id | tunnel_net      | dz_ip        | status    | owner
// NR8fpCK7mqeFVJ3mUmhndX2JtRCymZzgQgGj5JNbGp8  | IBRL                | la2-dz01 | GREOverDIA | 1.2.3.4      | 500       | 169.254.0.2/31  | 1.2.3.4      | activated | Dc3LFdWwKGJvJcVkXhAr14kh1HS6pN7oCWrvHfQtsHGe
// 5Rm8dp4dDzR5SE3HtrqGVpqHLaPvvxDEV3EotqPBBUgS | IBRL                | la2-dz01 | GREOverDIA | 5.6.7.8      | 504       | 169.254.0.10/31 | 5.6.7.8      | activated | Dc3LFdWwKGJvJcVkXhAr14kh1HS6pN7oCWrvHfQtsHGe
func mapFromTable(output []byte) []map[string]string {
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

	slices.SortFunc(sliceOfMaps, func(a, b map[string]string) int {
		return strings.Compare(strings.ToLower(a["account"]), strings.ToLower(b["account"]))
	})

	return sliceOfMaps
}
