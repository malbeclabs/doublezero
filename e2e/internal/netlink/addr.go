package netlink

import "strings"

func HasAddr(entries []map[string]any, ifname string, wantAddr string) bool {
	wantIP := strings.Split(wantAddr, "/")[0]

	for _, entry := range entries {
		if entry["ifname"] != ifname {
			continue
		}
		addrInfos, ok := entry["addr_info"].([]any)
		if !ok {
			continue
		}
		for _, ai := range addrInfos {
			infoMap, ok := ai.(map[string]any)
			if !ok {
				continue
			}
			if local, ok := infoMap["local"].(string); ok && local == wantIP {
				return true
			}
		}
	}
	return false
}
