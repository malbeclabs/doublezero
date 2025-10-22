package uping

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	CAP_NET_ADMIN = 12
	CAP_NET_RAW   = 13
)

// RequirePrivileges checks: root OR CAP_NET_RAW (and CAP_NET_ADMIN if binding to device).
func RequirePrivileges(bindingToIface bool) error {
	if os.Geteuid() == 0 {
		return nil
	}
	rawOK, err := hasCap(CAP_NET_RAW)
	if err != nil {
		return err
	}
	if !rawOK {
		return fmt.Errorf("requires CAP_NET_RAW (or root). grant with: sudo setcap cap_net_raw+ep /path/to/uping-send (and /path/to/uping-recv)")
	}
	if bindingToIface {
		adminOK, err := hasCap(CAP_NET_ADMIN)
		if err != nil {
			return err
		}
		if !adminOK {
			return fmt.Errorf("SO_BINDTODEVICE typically requires CAP_NET_ADMIN. grant with: sudo setcap cap_net_admin+ep /path/to/uping-send (and /path/to/uping-recv)")
		}
	}
	return nil
}

func hasCap(bit int) (bool, error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return false, err
	}
	defer f.Close()

	var capEffStr string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "CapEff:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				capEffStr = fields[1]
				break
			}
		}
	}
	if capEffStr == "" {
		return false, errors.New("CapEff not found in /proc/self/status")
	}

	val, err := strconv.ParseUint(capEffStr, 16, 64)
	if err != nil {
		return false, err
	}
	return (val & (1 << uint(bit))) != 0, nil
}
